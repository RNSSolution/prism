# Color Prism

Color Prism Consensus algorithm

The Color Prism Consensus algorithm will be the backbone of the Color Platform, ensuring
dApp and Core transaction flows happen accordingly. Using the Color Consensus we believe it
will achieve two main things:

1. Achieve Transaction Confirmation Speeds at **twice** the rate of PBFT through
clustered transaction confirmation.
2. More Decentralized than other DPoS networks, thus being more Robust and
harder to bribe the mining consensus participants.

For protocol details, see [the beige paper](https://color-platform.org/~colors/_assets/down/Color_Prism.pdf).

## Note on Current Development Stage

Prism is being actively developed and the current version is a draft implementation of 3 leagues with 3 nodes each.

## Minimum requirements

Prerequisites for running the local network:
1. Linux host (tested in Ubuntu 18.04 guest in Virtual Box with 8 Gb RAM)
2. Installed [docker](https://docs.docker.com/install/) and [docker-compose](https://docs.docker.com/compose/install/)
3. Go version 1.11.4 or higher

## Install Prism

### Get Source Code
```
mkdir -p $GOPATH/src/github.com/ColorPlatform
cd $GOPATH/src/github.com/ColorPlatform
git clone https://github.com/ColorPlatform/prism.git
cd prism
```

### Install tools and dependencies
```
make get_tools
make get_vendor_deps
```

### Compile
```
make build
```

### Run local network with 3 leagues each by 3 nodes
```
make build-docker-localnode
make localnet-start-3x3
```

### Stop the network
```
make localnet-stop
```
