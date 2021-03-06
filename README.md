dcrstakesim
===========

dcrstakesim provides a tool for simulating the full Decred proof-of-stake system
behavior.  The primary goal and purpose of this tool is to allow different
ticket price (aka stake difficulty) algorithms to be accurately modelled under
reasonably realistic scenarios.

It includes all relevant behavior such as calculating ticket prices, tracking
correct subsidy generation (including proper reduction based on the number of
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

- **Go 1.12 or 1.13**

  Installation instructions can be found here: http://golang.org/doc/install.
  It is recommended to add `$GOPATH/bin` to your `PATH` at this point.

- **Git**

  Installation instructions can be found at https://git-scm.com or
  https://gitforwindows.org.

### Building/Installing the latest release directly from remote repository

The `go` tool is used to obtain, build, and install (to `GOPATH`) the project.

To build and install directly from the remote repository:

* Set the `GO111MODULE=on` environment variable
* Run `go get -v github.com/davecgh/dcrstakesim`
* Optionally run `go get -v github.com/davecgh/dcrstakesim/extractdata` if you
  plan to run a mainnet data-driven simulation instead of a full simulation
* Note that the `dcrstakesim` (and optional `extractdata`) executable will be
  installed to `$GOPATH/bin`.  `GOPATH` defaults to `$HOME/go` (or
  `%USERPROFILE%\go` on Windows) if unset.

#### Example of obtaining and building the latest release on Windows 10:

```PowerShell
PS> $env:GO111MODULE="on"
PS> go get -v github.com/davecgh/dcrstakesim
PS> & "$(go env GOPATH)\bin\dcrstakesim" -h
```
#### Example of obtaining and building the latest release on Linux:

```bash
$ GO111MODULE=on go get -v github.com/davecgh/dcrstakesim
$ $(go env GOPATH)/bin/dcrstakesim -h
```

### Obtaining source and building/installing locally

Similar to building and installing the latest release directly from the remote
repository, the `go` tool is used to build and install (to `GOPATH`) the project
from a local copy of the repository.

To build and install from a local checkout of the repository:

* Set the `GO111MODULE=on` environment variable if building from within `GOPATH`
* Ensure you are in the checked-out repository's root directory
* Run `go install`
* Optionally run `go install ./extractdata` if you plan to run a mainnet
  data-driven simulation instead of a full simulation
* Note that the `dcrstakesim` and optional `extractdata` executable will be
  installed to `$GOPATH/bin`.  `GOPATH` defaults to `$HOME/go` (or
  `%USERPROFILE%\go` on Windows) if unset.

#### Example of cloning the repo and building from it on Windows 10:

```PowerShell
PS> git clone https://github.com/davecgh/dcrstakesim $env:USERPROFILE\src\dcrstakesim
PS> cd $env:USERPROFILE\src\dcrstakesim
PS> go install .
PS> & "$(go env GOPATH)\bin\dcrstakesim" -h
```

#### Example of cloning the repo and building from it on Linux:

```bash
$ git clone https://github.com/davecgh/dcrstakesim ~/src/dcrstakesim
$ cd ~/src/dcrstakesim
$ go install .
$ $(go env GOPATH)\bin\dcrstakesim -h
```

## Issue Tracker

The [integrated github issue tracker](https://github.com/davecgh/dcrstakesim/issues)
is used for this project.

## License

dcrstakesim is licensed under the [copyfree](http://copyfree.org) ISC License.
