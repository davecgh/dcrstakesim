dcrstakesim
===========

dcrstakesim provides a tool for simulating the full Decred proof-of-stake system
behavior.  The primary goal and purpose of this tool is to allow different
ticket price (aka stake difficulty) algorithms to be accurately modelled under
reasonably realistic scenarios.

It includes all relevant behavior such as calculating ticket prices, tracking
correct susbsidy generation (including proper reduction based on the number of
votes in a block), keeping a running tally of the spendable subsidy (including
maturity requirements), maintaining the pool of live tickets, selecting winning
tickets via the same deterministic algorithm the real network uses, expiring
tickets that aren't selected, revoking tickets that are either missed or
expired, and ticket purchasing behavior.

The simulation only contains the current mainnet ticket price algorithm as of
March 2017.  It is intended that proposed algorithms are added to the code and
the simulation be updated to call the new algorithm to produce the results.

Two separate modes are supported:

  1. Full Simulation (default) - This mode fully automates the simulation by
     making ticket purchase decisions based on upon a demand distribution
     function.

  2. Mainnet Data-driven Simulation - This mode accepts a CSV file which is
     expected to contain data extracted from mainnet in order to drive the
	 simulation according to actual historical mainnet data.  The result is a
     reproduction of exactly what has already happened on mainnet up to the
	 current time and helps prove the correctness of the simulation.  Use
	 -inputcsv=mainnetdata.csv to use this mode.  The mainnetdata.csv file can
	 be extracted by using the `extractdata` utility.

## Installation and updating

### Windows/Linux/BSD/POSIX - Build from source

Building or updating from source requires the following build dependencies:

- **Go 1.7 or 1.8**

  Installation instructions can be found here: http://golang.org/doc/install.
  It is recommended to add `$GOPATH/bin` to your `PATH` at this point.

- **Glide**

  Glide is used to manage project dependencies and provide reproducible builds.
  It is recommended to use the latest Glide release, unless a bug prevents doing
  so.  The latest releases (for both binary and source) can be found
  [here](https://github.com/Masterminds/glide/releases).

Unfortunately, the use of `glide` prevents a handy tool such as `go get` from
automatically downloading, building, and installing the source in a single
command.  Instead, the latest project and dependency sources must be first
obtained manually with `git` and `glide`, and then `go` is used to build and
install the project.

**Getting the source**:

For a first time installation, the project and dependency sources can be
obtained manually with `git` and `glide` (create directories as needed):

```
git clone https://github.com/davecgh/dcrstakesim $GOPATH/src/github.com/davecgh/dcrstakesim
cd $GOPATH/src/github.com/davecgh/dcrstakesim
glide install
```

To update an existing source tree, pull the latest changes and install the
matching dependencies:

```
cd $GOPATH/src/github.com/davecgh/dcrstakesim
git pull
glide install
```

**Building/Installing**:

The `go` tool is used to build or install (to `GOPATH`) the project.  Some
example build instructions are provided below (all must run from the
`dcrstakesim` project directory).

To build a `dcrstakesim` executable and install it to `$GOPATH/bin/`:

```
go install
```

To build a `dcrstakesim` executable and place it in the current directory:

```
go build
```

## Issue Tracker

The [integrated github issue tracker](https://github.com/davecgh/dcrstakesim/issues)
is used for this project.

## License

dcrstakesim is licensed under the [copyfree](http://copyfree.org) ISC License.
