# swaplistener
go program for listening to swaps on EVM chains

![demo](https://user-images.githubusercontent.com/107820179/174557681-cda49e44-605a-4e06-8872-0674e9528d85.png)

the source code is `main.go`

# building the executable
Run `go build .` in the main directory. 
If all goes well, this should produce the `swaplistener` executable file.

# running the executable
First you need to generate `ram.data` from the included bootstrap file `bootstrap.data`

`./swaplistener --bootstrap`
or, more verbosely:
`./swaplistener --bootstrap -in bootstrap.data -ram ram.data`

Then simply run `./swaplistener` to start the listener. 

# filter results
If you only want to listen to a subset of the pairs, add `-q` flags, e.g.,
`./swaplistener -q MAGIK -q MIM:WINE`
will only listen to pairs which have MAGIK as either pair, or are MIM:WINE.

# customizations

The data stored in the ram.data file can be personalized. For instance, if you want to switch the "direction" of a pair, you can change the normal parameter to `false`
