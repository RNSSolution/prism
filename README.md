# Color Prism

The Color Prism Consensus algorithm will be the backbone of the Color Platform, ensuring
dApp and Core transaction flows happen accordingly. Using the Color Consensus we believe it
will achieve two main things:

1. Achieve Transaction Confirmation Speeds at **twice** the rate of PBFT through
clustered transaction confirmation.
2. More Decentralized than other DPoS networks, thus being more Robust and
harder to bribe the mining consensus participants.

## Technical Details

|     |     |
| --- | --- |
| **Consensus Algorithm Type** | Modified DPoS |
| **Number of Consensus Participants** | 49 (Main) 28 (Backup) |
| **Block interval** | 0.5 Second |
| **Block Confirmation** | 1.0 Second |
| **Maximum Block Size** | 1.0 Mega byte |
| **TPS (Transactions per second)** | 2000-4000 |

For further details, see [the beige paper](https://color-platform.org/~colors/_assets/down/Color_Prism.pdf).

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

### Run local network

#### Prerequisites
Local network is based on [Docker](https://docs.docker.com/install/) infrastructure and [`docker-compose` tool](https://docs.docker.com/compose/install/).
1. Make sure you installed the most recent versions of `docker` and `docker-compose`.
1. Add your Linux user to the `docker` group
    ```
    sudo usermod -a -G docker $USER
    ```
1. Build docker image
    ```
    make build-docker-localnode
    ```

#### Run local network
Let's consider running a local network with 9 nodes grouped in 3 leagues

1. Generate configuration files
    ```
    ./scripts/localnet -l 3 -n 3 init
    ```
2. Run the local network
    ```
    ./scripts/localnet start
    ```
#### Stop the network
To stop the local network gracefully use the following command:
```
./scripts/localnet stop
```
