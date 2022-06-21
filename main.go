package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fatih/color"
)

// Command Line Flags.
var bootstrapFlag = flag.Bool("bootstrap", false, "set this to fetch metadata using a bootstrap file")
var generateBootstrapFlag = flag.Bool("gen_bootstrap", false, "set this to generate bootstrap.data using the current ram file")
var ramFlag = flag.String("ram", "ram.data", "file name for ram")
var bootstrapFileFlag = flag.String("in", "bootstrap.data", "file name for bootstrap")

// queryArray... an array of queries given by -q flags
type queryArray []string

func (q *queryArray) String() (str string) {
	for _, val := range *q {
		str += fmt.Sprintf(" %s", val)
	}
	return
}
func (q *queryArray) Set(value string) (err error) {
	*q = append(*q, value)
	return nil
}

var queryFlag queryArray

// the main method. this is what is run when the program is executed.
func main() {
	flag.Var(&queryFlag, "q", "queries SYMBOL0:SYMBOL1")
	flag.Parse()
	var contract abi.ABI
	contract, _ = abi.JSON(strings.NewReader(lp_abi))
	var ram map[common.Address]Pair
	var header map[int64]interface{}
	if *bootstrapFlag {
		fmt.Printf("Load addresses in %s and fetch data? [press enter]", *bootstrapFileFlag)
		fmt.Scanln()
		fmt.Print("Fetching data from blockchain...\n")
		if tmph, tmp, err := load_bootstrap_file(*bootstrapFileFlag); err == nil {
			ram = tmp
			header = tmph
		} else {
			panic(err)
		}
		fmt.Printf("Success! Overwrite %s with fetched data? [press enter]", *ramFlag)
		fmt.Scanln()
		fmt.Printf("Overwriting %s...\n", *ramFlag)
		if err := save_ram_to_ram_file(header, ram, *ramFlag); err == nil {
			fmt.Println("Successs!")
			os.Exit(0)
		} else {
			panic(err)
		}
	} else {
		if tmph, tmp, err := load_ram_from_ram_file(*ramFlag); err == nil {
			ram = tmp
			header = tmph
			if *generateBootstrapFlag {
				if err := save_bootstrap_file(header, ram, *bootstrapFileFlag); err == nil {
				} else {
					panic(err)
				}
				fmt.Printf("Successfully generated %s\n", *bootstrapFileFlag)
				os.Exit(0)
			}
		} else {
			fmt.Printf("Make sure %s is in the current directory\n", *ramFlag)
			fmt.Printf("Run with -bootstrap flag and %s\n", *bootstrapFileFlag)
			panic(err)
		}
	}
	var d int
	addresses := make(map[int64][]common.Address)
	for key, val := range ram {
		// filtering by query
		s0 := strings.ToLower(val.S0)
		s1 := strings.ToLower(val.S1)
		var b bool
		for _, q := range queryFlag {
			ss := strings.Split(q, ":")
			switch len(ss) {
			case 1:
				a := strings.ToLower(ss[0])
				b = b || strings.HasPrefix(s0, a) || strings.HasPrefix(s1, a)
			case 2:
				a0 := strings.ToLower(ss[0])
				a1 := strings.ToLower(ss[1])
				bt0 := strings.HasPrefix(s0, a0) && strings.HasPrefix(s1, a1)
				bt1 := strings.HasPrefix(s1, a0) && strings.HasPrefix(s0, a1)
				b = b || bt0 || bt1
			}
		}
		if !b && len(queryFlag) > 0 {
			continue
		} else {
			if d < len(s0) {
				d = len(s0)
			}
			if d < len(s1) {
				d = len(s1)
			}
			a := addresses[val.Chain]
			a = append(a, key)
			addresses[val.Chain] = a
		}
	}
	topics := contract.Events
	id_swap := topics["Swap"].ID
	id_mint := topics["Mint"].ID
	id_burn := topics["Burn"].ID
	logs := make(chan types.Log)
	errs := make(chan error)
	var wg sync.WaitGroup
	for key, val := range addresses {
		head := header[key].(map[string]interface{})
		query := ethereum.FilterQuery{
			Addresses: val,
			Topics:    [][]common.Hash{{id_swap, id_mint, id_burn}},
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if wss, ok := head["wss"].(string); wss != "" && ok == true {
				this_chain_logs := make(chan types.Log)
				fmt.Printf("dialing %s blockchain...\n", head["name"].(string))
				client, err := ethclient.Dial(wss)
				if err != nil {
					log.Fatal(err)
				}
				if tmp, err := client.SubscribeFilterLogs(context.Background(), query, this_chain_logs); err == nil {
					go func() {
						for {
							select {
							case err := <-tmp.Err():
								errs <- err
							case log := <-this_chain_logs:
								logs <- log
							}
						}
					}()
				}
			}
		}()
	}
	wg.Wait()
	fmt.Println("successfully initialized! listening for swap events...")
	for {
		select {
		case err := <-errs:
			log.Fatal(err)
		case vLog := <-logs:
			vLog_handler(ram, contract, vLog, d)
		}
	}
}

func vLog_handler(ram map[common.Address]Pair, contract abi.ABI, vLog types.Log, d int) {
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
			s, c = p.String(d)
		}
	case "Mint":
		if f, err := contract.Unpack("Mint", vLog.Data); err == nil {
			p.mintUpdate(f)
			s, c = p.String(d)
		}
	case "Burn":
		if f, err := contract.Unpack("Burn", vLog.Data); err == nil {
			p.burnUpdate(f)
			s, c = p.String(d)
		}
	}
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)
	c.Printf("%s | %s @ %s | %s\n", s, now.Format("15:04:05"), vLog.Address.String()[:6], fmt.Sprintf("%#x", vLog.TxHash)[:6])
}

// amt0 should be the "stable" half of the pair (depending on the context...)
// amt0 amt1 will constantly be updated
// B is the "normal" parameter which can be changed (change this in the ram file) to flip the "direction" of a pair.
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

// uses the raw *big.Int data and generates *big.Float data, appropriately renormalized using the "decimal" data
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

// prints a pair, returns the string and the color.
func (p *Pair) String(d int) (s string, c *color.Color) {
	amt0f, amt1f, price := p.amts()
	var t1, t2, t3 string
	my_red := color.New(color.FgHiRed)
	my_green := color.New(color.FgGreen)
	my_cyan := color.New(color.FgCyan)
	my_yellow := color.New(color.FgYellow)

	switch {
	case p.mode == 0:
		t1 = fmt.Sprintf("%12.4f %-*s  ->", amt0f, d+1, p.S0)
		t2 = fmt.Sprintf("->%12.4f %-*s", amt1f, d, p.S1)
		if p.B == true {
			c = my_green
		} else {
			c = my_red
		}
	case p.mode == 1:
		t1 = fmt.Sprintf("%12.4f %-*s  ->", amt1f, d+1, p.S1)
		t2 = fmt.Sprintf("->%12.4f %-*s", amt0f, d, p.S0)
		c = my_red
		if p.B == true {
			c = my_red
		} else {
			c = my_green
		}
	case p.mode == 2:
		t1 = fmt.Sprintf("%12.4f %-*s  ->", amt0f, d+1, p.S0)
		t2 = fmt.Sprintf("<-%12.4f %-*s", amt1f, d, p.S1)
		c = my_cyan
	case p.mode == 3:
		t1 = fmt.Sprintf("%12.4f %-*s  <-", amt0f, d+1, p.S0)
		t2 = fmt.Sprintf("->%12.4f %-*s", amt1f, d, p.S1)
		c = my_yellow
	}
	t3 = fmt.Sprintf("%9.4f", price)
	s = fmt.Sprintf("%s %s | %s", t1, t2, t3)
	return
}

// the following three functions update the pair variable "p" based on the data in the Log Event. Will switch the mode of p depending on the event.
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

// fetches the symbol data by querying blockchain... part of bootstrap
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

// uses ethCall() to get the pair of tokens by querying the LP contract... part of bootstrap
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

// uses ethCall() to get decimal information from blockchain... part of bootstrap
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

// loads ram from ram file
func load_ram_from_ram_file(filename string) (header map[int64]interface{}, ram map[common.Address]Pair, err error) {
	if f, ok := os.Open(filename); os.IsNotExist(ok) {
		f.Close()
		return
	} else {
		defer f.Close()
		r := json.NewDecoder(f)
		var j [2]interface{}
		err = r.Decode(&j)
		if err != nil {
			return
		}
		header = make(map[int64]interface{})
		ram = make(map[common.Address]Pair)
		b1, b2 := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
		t1, t2 := json.NewEncoder(b1), json.NewEncoder(b2)
		t1.Encode(j[0])
		t2.Encode(j[1])
		s1, s2 := json.NewDecoder(b1), json.NewDecoder(b2)
		s1.Decode(&header)
		s2.Decode(&ram)
	}
	return
}

// saves ram to a ram file
func save_ram_to_ram_file(header map[int64]interface{}, ram map[common.Address]Pair, filename string) (err error) {
	f, err := os.Create(filename)
	defer f.Close()
	if err != nil {
		return
	}
	r := json.NewEncoder(f)
	r.SetIndent("", "  ")
	err = r.Encode([]interface{}{header, ram})
	return
}

// uses ram file to create a bootstrap file
func save_bootstrap_file(header map[int64]interface{}, ram map[common.Address]Pair, filename string) (err error) {
	f, err := os.Create(filename)
	defer f.Close()
	if err != nil {
		return
	}
	r := json.NewEncoder(f)
	r.SetIndent("", "  ")
	bootstrap := make(map[int64]interface{})
	for key, val := range ram {
		if try, ok := bootstrap[val.Chain].(map[string]interface{}); ok == false {
			try = make(map[string]interface{})
			head := header[val.Chain].(map[string]interface{})
			try["name"] = head["name"].(string)
			try["url"] = head["url"].(string)
			try["wss"] = head["wss"].(string)
			try["data"] = []string{key.String()}
			bootstrap[val.Chain] = try
		} else {
			s := try["data"].([]string)
			s = append(s, key.String())
			try["data"] = s
			bootstrap[val.Chain] = try
		}
	}
	err = r.Encode(bootstrap)
	return
}

// loads the bootstrap file by calling the blockchain to get the symbol and decimal metadata
func load_bootstrap_file(filename string) (ram_header map[int64]interface{}, ram map[common.Address]Pair, err error) {
	bootstrap := make(map[int64]interface{})
	if f, ok := os.Open(filename); ok != nil {
		f.Close()
		err = ok
		return
	} else {
		defer f.Close()
		r := json.NewDecoder(f)
		j := make(map[int64]interface{})
		err = r.Decode(&j)
		if err != nil {
			return
		}
		bootstrap = j
	}

	ram_header = make(map[int64]interface{})
	ram = make(map[common.Address]Pair)
	for key, val := range bootstrap {
		header := make(map[string]string)
		var data []interface{}
		var url string
		if try, ok := val.(map[string]interface{}); ok == true {
			data = try["data"].([]interface{})
			header["wss"] = try["wss"].(string)
			url = try["url"].(string)
			header["url"] = url
			header["name"] = try["name"].(string)
		}
		ram_header[key] = header
		for _, lpaddr_i := range data {
			lpaddr := lpaddr_i.(string)
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

// ethCall is used to make a one-off json request to the blockchain
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

// auxiliary types used in ethCall()
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

// low level function for making json request
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
	// fmt.Printf("%s\n", rbody)
	if err != nil {
		return
	}
	defer response.Body.Close()
	return
}

// the ABI for standard LP contract
const lp_abi = `[{"inputs":[],"payable":false,"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":true,"internalType":"address","name":"spender","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount0","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1","type":"uint256"},{"indexed":true,"internalType":"address","name":"to","type":"address"}],"name":"Burn","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount0","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1","type":"uint256"}],"name":"Mint","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount0In","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1In","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount0Out","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"amount1Out","type":"uint256"},{"indexed":true,"internalType":"address","name":"to","type":"address"}],"name":"Swap","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint112","name":"reserve0","type":"uint112"},{"indexed":false,"internalType":"uint112","name":"reserve1","type":"uint112"}],"name":"Sync","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"from","type":"address"},{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Transfer","type":"event"},{"constant":true,"inputs":[],"name":"DOMAIN_SEPARATOR","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"MINIMUM_LIQUIDITY","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"PERMIT_TYPEHASH","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"address","name":"","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"}],"name":"burn","outputs":[{"internalType":"uint256","name":"amount0","type":"uint256"},{"internalType":"uint256","name":"amount1","type":"uint256"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"factory","outputs":[{"internalType":"address","name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"getReserves","outputs":[{"internalType":"uint112","name":"_reserve0","type":"uint112"},{"internalType":"uint112","name":"_reserve1","type":"uint112"},{"internalType":"uint32","name":"_blockTimestampLast","type":"uint32"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"_token0","type":"address"},{"internalType":"address","name":"_token1","type":"address"}],"name":"initialize","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"kLast","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"}],"name":"mint","outputs":[{"internalType":"uint256","name":"liquidity","type":"uint256"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"nonces","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"},{"internalType":"uint256","name":"deadline","type":"uint256"},{"internalType":"uint8","name":"v","type":"uint8"},{"internalType":"bytes32","name":"r","type":"bytes32"},{"internalType":"bytes32","name":"s","type":"bytes32"}],"name":"permit","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"price0CumulativeLast","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"price1CumulativeLast","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"}],"name":"skim","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"internalType":"uint256","name":"amount0Out","type":"uint256"},{"internalType":"uint256","name":"amount1Out","type":"uint256"},{"internalType":"address","name":"to","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"swap","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[],"name":"sync","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"token0","outputs":[{"internalType":"address","name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"token1","outputs":[{"internalType":"address","name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"value","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"}]`
