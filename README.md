# swaplistener
go program for listening to swaps on EVM chains

![swap_listener](https://user-images.githubusercontent.com/107820179/174509833-d50f1680-9181-4169-b37a-08d3578e0a03.png)

the source code is `main.go`

# building the executable
Run `go build .` in the main directory. 
If all goes well, this should produce the `swaplistener` executable file.

# running the executable
First you need to generate `ram.data` from the included bootstrap file `bootstrap.data`

`./swap_listener --bootstrap`

Then simply run `./swaplistener` to start the listener. 

# customizations

The data stored in the ram.data file can be personalized. For instance, if you want to switch the "direction" of a pair, you can change the normal parameter to `false`
