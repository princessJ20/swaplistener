package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fatih/color"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"
)

const ftm_url = "https://rpc.ftm.tools"
const avax_url = "https://api.avax.network/ext/bc/C/rpc"

const ftm_chain_id = int64(250)
const avax_chain_id = int64(43114)

const avax_wss = "wss://api.avax.network/ext/bc/C/ws"
const ftm_wss = "wss://wsapi.fantom.network/"

var bootstrapFlag = flag.Bool("bootstrap", false, "set this to fetch metadata using a bootstrap file")
var generateBootstrapFlag = flag.Bool("gen_bootstrap", false, "set this to generate bootstrap.data using the current ram file")

func main() {
	fmt.Println("dialing FTM and AVAX blockchains...")
	flag.Parse()
	avax_wss := avax_wss
	ftm_wss := ftm_wss
	var contract abi.ABI

	contract, _ = abi.JSON(strings.NewReader(lp_abi))
	avax_client, err := ethclient.Dial(avax_wss)
	ftm_client, err := ethclient.Dial(ftm_wss)
	if err != nil {
		log.Fatal(err)
	}
	var ram map[common.Address]Pair
	if *bootstrapFlag {
		fmt.Print("Load addresses in bootstrap.data and fetch data? [press enter]")
		fmt.Scanln()
		fmt.Print("Fetching data from blockchain...\n")
		if tmp, err := load_bootstrap_file("bootstrap.data"); err == nil {
			ram = tmp
		} else {
			panic(err)
		}
		fmt.Print("Success! Overwrite ram.data with fetched data? [press enter]")
		fmt.Scanln()
		fmt.Print("Overwriting ram.data...\n")
		if err := save_ram_to_ram_file(ram, "ram.data"); err == nil {
			fmt.Println("Successs!")
			os.Exit(0)
		} else {
			panic(err)
		}
	} else {
		if tmp, err := load_ram_from_ram_file("ram.data"); err == nil {
			ram = tmp
			if *generateBootstrapFlag {
				if err := save_bootstrap_file(ram, "bootstrap.data"); err == nil {
				} else {
					panic(err)
				}
			}
		} else {
			fmt.Println("Make sure ram.data is in the current directory")
			fmt.Println("Run with -bootstrap flag and bootstrap.data")
			panic(err)
		}
	}

	var avax_addresses, ftm_addresses []common.Address
	for key, val := range ram {
		if val.Chain == avax_chain_id {
			avax_addresses = append(avax_addresses, key)
		} else {
			ftm_addresses = append(ftm_addresses, key)
		}
	}

	topics := contract.Events
	id_swap := topics["Swap"].ID
	id_mint := topics["Mint"].ID
	id_burn := topics["Burn"].ID
	avax_query := ethereum.FilterQuery{
		Addresses: avax_addresses,
		Topics:    [][]common.Hash{{id_swap, id_mint, id_burn}},
	}
	ftm_query := ethereum.FilterQuery{
		Addresses: ftm_addresses,
		Topics:    [][]common.Hash{{id_swap, id_mint, id_burn}},
	}
	avax_logs, ftm_logs := make(chan types.Log), make(chan types.Log)
	avax_sub, err := avax_client.SubscribeFilterLogs(context.Background(), avax_query, avax_logs)
	ftm_sub, err := ftm_client.SubscribeFilterLogs(context.Background(), ftm_query, ftm_logs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("successfully initialized! listening on the blockchain for swap events...")
	for {
		select {
		case err := <-avax_sub.Err():
			log.Fatal(err)
		case err := <-ftm_sub.Err():
			log.Fatal(err)
		case vLog := <-avax_logs:
			vLog_handler(ram, contract, vLog)
		case vLog := <-ftm_logs:
			vLog_handler(ram, contract, vLog)
		}
	}
}

func vLog_handler(ram map[common.Address]Pair, contract abi.ABI, vLog types.Log) {
	e, err := contract.EventByID(vLog.Topics[0])
	if err != nil {
		panic(err)
	}
	p := ram[vLog.Address]
	var c *color.Color
	var s string
	switch e.Name {
	case "Swap":
		if f, err := contract.Unpack("Swap", vLog.Data); err == nil {
			p.swapUpdate(f)
			s, c = p.String()
		}
	case "Mint":
		if f, err := contract.Unpack("Mint", vLog.Data); err == nil {
			p.mintUpdate(f)
			s, c = p.String()
		}
	case "Burn":
		if f, err := contract.Unpack("Burn", vLog.Data); err == nil {
			p.burnUpdate(f)
			s, c = p.String()
		}
	}
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)
	c.Printf("%s | %s @ %s | %s\n", s, now.Format("15:04:05"), vLog.Address.String()[:6], fmt.Sprintf("%#x", vLog.TxHash)[:6])
}

// amt0 should be the stable pair
// amt0 amt1 will constantly be updated
type Pair struct {
	S0    string `json:"symbol0"`
	S1    string `json:"symbol1"`
	D0    byte   `json:"decimals0"`
	D1    byte   `json:"decimals1"`
	amt0  *big.Int
	amt1  *big.Int
	Chain int64 `json:"chainID"`
	B     bool  `json:"normal"`
	// one byte for the mode: 0 buy 1 sell 2 make 3 break
	mode byte
}

func (p *Pair) amts() (amt0f *big.Float, amt1f *big.Float, price *big.Float) {
	exp0 := (&big.Float{}).SetInt((&big.Int{}).Exp(big.NewInt(10), big.NewInt(int64(p.D0)), nil))
	exp1 := (&big.Float{}).SetInt((&big.Int{}).Exp(big.NewInt(10), big.NewInt(int64(p.D1)), nil))
	price = big.NewFloat(0)
	amt0f = (&big.Float{}).SetInt(p.amt0)
	amt1f = (&big.Float{}).SetInt(p.amt1)
	amt0f.Quo(amt0f, exp0)
	amt1f.Quo(amt1f, exp1)
	if amt0f.Cmp(big.NewFloat(0)) > 0 {
		if p.B {
			price.Quo(amt0f, amt1f)
		} else {
			price.Quo(amt1f, amt0f)
		}
	}
	return
}

func (p *Pair) String() (s string, c *color.Color) {
	amt0f, amt1f, price := p.amts()
	var t1, t2, t3 string
	my_red := color.New(color.FgHiRed)
	my_green := color.New(color.FgGreen)
	my_cyan := color.New(color.FgCyan)
	my_yellow := color.New(color.FgYellow)

	switch {
	case p.mode == 0:
		t1 = fmt.Sprintf("%12.4f %-7s  ->", amt0f, p.S0)
		t2 = fmt.Sprintf("->%12.4f %-7s", amt1f, p.S1)
		if p.B == true {
			c = my_green
		} else {
			c = my_red
		}
	case p.mode == 1:
		t1 = fmt.Sprintf("%12.4f %-7s  ->", amt1f, p.S1)
		t2 = fmt.Sprintf("->%12.4f %-7s", amt0f, p.S0)
		c = my_red
		if p.B == true {
			c = my_red
		} else {
			c = my_green
		}
	case p.mode == 2:
		t1 = fmt.Sprintf("%12.4f %-7s  ->", amt0f, p.S0)
		t2 = fmt.Sprintf("<-%12.4f %-7s", amt1f, p.S1)
		c = my_cyan
	case p.mode == 3:
		t1 = fmt.Sprintf("%12.4f %-7s  <-", amt0f, p.S0)
		t2 = fmt.Sprintf("->%12.4f %-7s", amt1f, p.S1)
		c = my_yellow
	}
	t3 = fmt.Sprintf("%9.4f", price)
	s = fmt.Sprintf("%s %s | %s", t1, t2, t3)
	return
}

func (p *Pair) swapUpdate(f []interface{}) {
	var ell [4]*big.Int
	for i, k := range f {
		ell[i] = k.(*big.Int)
	}
	if ell[0].Cmp(big.NewInt(0)) == 0 {
		// SELL
		p.amt0 = ell[2]
		p.amt1 = ell[1]
		p.mode = 1
	} else {
		// BUY
		p.amt0 = ell[0]
		p.amt1 = ell[3]
		p.mode = 0
	}
}
func (p *Pair) mintUpdate(f []interface{}) {
	var ell [2]*big.Int
	for i, k := range f {
		ell[i] = k.(*big.Int)
	}
	p.amt0 = ell[0]
	p.amt1 = ell[1]
	p.mode = 2
}
func (p *Pair) burnUpdate(f []interface{}) {
	var ell [2]*big.Int
	for i, k := range f {
		ell[i] = k.(*big.Int)
	}
	p.amt0 = ell[0]
	p.amt1 = ell[1]
	p.mode = 3
}

func fetch_symbol(coin_addr string, schan chan string, url string) {
	tmpabi, _ := abi.JSON(strings.NewReader(`[{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"}]`))
	packed_bytes_0, _ := tmpabi.Pack("symbol")
	result := ethCall(url, coin_addr, fmt.Sprintf("%#x", packed_bytes_0))
	if body, err := hex.DecodeString(result[2:]); err == nil {
		if f, err := tmpabi.Unpack("symbol", body); err == nil {
			schan <- f[0].(string)
		} else {
			schan <- "ERROR"
		}
	}

}

func fetch_tokens(lp_addr string, tok0c chan string, tok1c chan string, url string) {
	tmpabi, _ := abi.JSON(strings.NewReader(`[{"inputs":[],"name":"token0","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"token1","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"}]`))
	go func() {
		packed_bytes, _ := tmpabi.Pack("token0")
		result := ethCall(url, lp_addr, fmt.Sprintf("%#x", packed_bytes))
		if body, err := hex.DecodeString(result[2:]); err == nil {
			if f, err := tmpabi.Unpack("token0", body); err == nil {
				for {
					tok0c <- (f[0].(common.Address)).String()
				}
			} else {
				panic(err)
			}
		}
	}()
	go func() {
		packed_bytes, _ := tmpabi.Pack("token1")
		result := ethCall(url, lp_addr, fmt.Sprintf("%#x", packed_bytes))
		if body, err := hex.DecodeString(result[2:]); err == nil {
			if f, err := tmpabi.Unpack("token1", body); err == nil {
				for {
					tok1c <- (f[0].(common.Address)).String()
				}
			} else {
				panic(err)
			}
		}
	}()
}
func fetch_decimals(coin_addr string, dchan chan byte, url string) {
	tmpabi, _ := abi.JSON(strings.NewReader(`[{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"}]`))
	packed_bytes_0, _ := tmpabi.Pack("decimals")
	result := ethCall(url, coin_addr, fmt.Sprintf("%#x", packed_bytes_0))
	if body, err := hex.DecodeString(result[2:]); err == nil {
		if f, err := tmpabi.Unpack("decimals", body); err == nil {
			dchan <- f[0].(byte)
		} else {
			dchan <- byte(18)
		}
	}
}

func load_ram_from_ram_file(filename string) (ram map[common.Address]Pair, err error) {
	if f, ok := os.Open(filename); os.IsNotExist(ok) {
		f.Close()
		return
	} else {
		defer f.Close()
		r := json.NewDecoder(f)
		j := make(map[common.Address]Pair)
		err = r.Decode(&j)
		if err != nil {
			return
		}
		ram = j
	}
	return
}
func save_ram_to_ram_file(ram map[common.Address]Pair, filename string) (err error) {
	f, err := os.Create(filename)
	defer f.Close()
	if err != nil {
		return
	}
	r := json.NewEncoder(f)
	r.SetIndent("", "  ")
	err = r.Encode(ram)
	return
}
func save_bootstrap_file(ram map[common.Address]Pair, filename string) (err error) {
	f, err := os.Create(filename)
	defer f.Close()
	if err != nil {
		return
	}
	r := json.NewEncoder(f)
	r.SetIndent("", "  ")
	bootstrap := make(map[int64][]string)
	for key, val := range ram {
		bootstrap[val.Chain] = append(bootstrap[val.Chain], key.String())
	}
	err = r.Encode(bootstrap)
	return
}

func load_bootstrap_file(filename string) (ram map[common.Address]Pair, err error) {
	bootstrap := make(map[int64][]string)
	if f, ok := os.Open(filename); ok != nil {
		f.Close()
		err = ok
		return
	} else {
		defer f.Close()
		r := json.NewDecoder(f)
		j := make(map[int64][]string)
		err = r.Decode(&j)
		if err != nil {
			return
		}
		bootstrap = j
	}

	ram = make(map[common.Address]Pair)
	for key, val := range bootstrap {
		var url string
		switch key {
		case ftm_chain_id:
			url = ftm_url
		case avax_chain_id:
			url = avax_url
		}
		for _, lpaddr := range val {
			t0c, t1c, s0c, s1c, d0c, d1c := make(chan string), make(chan string), make(chan string), make(chan string), make(chan byte), make(chan byte)
			go fetch_tokens(lpaddr, t0c, t1c, url)
			go fetch_symbol(<-t0c, s0c, url)
			go fetch_symbol(<-t1c, s1c, url)
			go fetch_decimals(<-t0c, d0c, url)
			go fetch_decimals(<-t1c, d1c, url)
			p := Pair{}
			p.S0 = <-s0c
			p.S1 = <-s1c
			p.D0 = <-d0c
			p.D1 = <-d1c
			p.Chain = key
			p.B = true
			ram[common.HexToAddress(lpaddr)] = p
		}
	}
	return
}

func ethCall(url string, addr string, data string) (result_string string) {
	result_string, _ = func() (str string, ok bool) {
		var myRequest json_request
		tmp1 := make(map[string]string)
		tmp1["to"] = addr
		tmp1["data"] = data
		tmpParams := []interface{}{tmp1, "latest"}
		myRequest = json_request{ID: time.Now().Add(69*time.Hour + 420*time.Nanosecond).Unix(), Method: "eth_call", JSONRPC: "2.0", Params: tmpParams}
		body, err := make_json_request(myRequest, url)
		if err != nil {
			return
		}
		var tmp json_response
		json.Unmarshal(body, &tmp)
		str, ok = tmp.Result.(string)
		return
	}()
	return
}

type json_response struct {
	ID      int64       `json:"id"`
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result"`
}

type json_request struct {
	ID      int64       `json:"id"`
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

const goerli_url = "http://localhost:8545"

func make_json_request(myRequest json_request, url string) (rbody []byte, err error) {
	var client http.Client
	body, err := json.Marshal(myRequest)
	if err != nil {
		return
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return
	}
	rbody, err = io.ReadAll(response.Body)
	if err != nil {
		return
	}
	defer response.Body.Close()
	return
}

const lp_abi = `[{"inputs":[],"payable":false,"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":true,"internalType":"address","name":"spender","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount0","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1","type":"uint256"},{"indexed":true,"internalType":"address","name":"to","type":"address"}],"name":"Burn","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount0","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1","type":"uint256"}],"name":"Mint","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount0In","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1In","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount0Out","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1Out","type":"uint256"},{"indexed":true,"internalType":"address","name":"to","type":"address"}],"name":"Swap","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint112","name":"reserve0","type":"uint112"},{"indexed":false,"internalType":"uint112","name":"reserve1","type":"uint112"}],"name":"Sync","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"from","type":"address"},{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Transfer","type":"event"},{"constant":true,"inputs":[],"name":"DOMAIN_SEPARATOR","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"MINIMUM_LIQUIDITY","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"PERMIT_TYPEHASH","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"address","name":"","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"}],"name":"burn","outputs":[{"internalType":"uint256","name":"amount0","type":"uint256"},{"internalType":"uint256","name":"amount1","type":"uint256"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"factory","outputs":[{"internalType":"address","name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"getReserves","outputs":[{"internalType":"uint112","name":"_reserve0","type":"uint112"},{"internalType":"uint112","name":"_reserve1","type":"uint112"},{"internalType":"uint32","name":"_blockTimestampLast","type":"uint32"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"_token0","type":"address"},{"internalType":"address","name":"_token1","type":"address"}],"name":"initialize","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"kLast","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"}],"name":"mint","outputs":[{"internalType":"uint256","name":"liquidity","type":"uint256"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"nonces","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"},{"internalType":"uint256","name":"deadline","type":"uint256"},{"internalType":"uint8","name":"v","type":"uint8"},{"internalType":"bytes32","name":"r","type":"bytes32"},{"internalType":"bytes32","name":"s","type":"bytes32"}],"name":"permit","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"price0CumulativeLast","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"price1CumulativeLast","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"}],"name":"skim","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"internalType":"uint256","name":"amount0Out","type":"uint256"},{"internalType":"uint256","name":"amount1Out","type":"uint256"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"swap","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[],"name":"sync","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"token0","outputs":[{"internalType":"address","name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"token1","outputs":[{"internalType":"address","name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"}]`
