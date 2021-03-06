# swaplistener
go program for listening to swaps on EVM chains

![demo1](https://user-images.githubusercontent.com/107820179/174716378-d7d5f5c3-f99d-4f33-9d42-c0fae408c03b.png)


the source code is all contained in the single file `main.go`

# output fields
```
      incoming                   outgoing          price        time     LP id    TX id
      0.0149 WINE     -> ->      2.8130 MIM     |  188.5075 | 23:21:33 @ 0x00cB | 0x9ebd
      0.1583 WINE     -> ->     29.8447 MIM     |  188.4817 | 23:21:33 @ 0x00cB | 0x9ebd
      0.1583 WINE     -> ->     29.8373 MIM     |  188.4345 | 23:21:33 @ 0x00cB | 0x9ebd
     29.8373 MIM      -> ->     28.7335 GRAPE   |    1.0384 | 23:21:33 @ 0xb382 | 0x9ebd
     29.7485 MIM      -> <-     28.7335 GRAPE   |    1.0353 | 23:21:33 @ 0xb382 | 0x9ebd
```
The arrows signify whether something is _entering_ or _exiting_ the liquidity pool. Thus `-> <-` signifies _making_ liquidity, while `<- ->` signifies _breaking_ liquidity (and `-> ->` is just a regular swap). 

# prerequisites
Need to have `go` installed. Follow the instructions at https://go.dev for your system. If you want to learn the `go` programming language, https://go.dev/tour/ is a good place to start.

# building the executable
Run `go build .` in the main directory. 
If all goes well, this should produce the `swaplistener` executable file.

# running the executable
First you need to generate `ram.data` from the included bootstrap file `bootstrap.data`. The bootstrap file contains a collection of LP token addresses. You can add pairs by adding the address of the LP token in the appropriate location in the bootstrap file (before running the bootstrap).

Running the bootstrap: `./swaplistener --bootstrap` (you only need to do this once)

Then simply run `./swaplistener` to start the listener. 

# filter results
If you only want to listen to a subset of the pairs, add `-q` flags, e.g.,
`./swaplistener -q MAGIK -q MIM:WINE`
will only listen to pairs which have MAGIK as either element in the pair, or are MIM:WINE.

# customizations

The data stored in the ram.data file can be personalized. For instance, if you want to switch the "direction" of a pair, you can change the `normal` parameter to `false`

# donations
donations may be sent to `0x56bdB5d2bfC30b7dE56095936984c9ce4b5b85C7`
