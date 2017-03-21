// Copyright (c) 2017 Dave Collins
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
)

const (
	// fieldsPerRecord defines the number of fields expected in each line
	// of the input CSV data.
	fieldsPerRecord = 3
)

// convertRecord converts the passed record, which is expected to be parsed from
// a CSV file, and thus will be a slice of strings, into a struct with concrete
// types.
func convertRecord(record []string) (*simData, error) {
	headerBytes, err := hex.DecodeString(record[1])
	if err != nil {
		return nil, err
	}

	var header wire.BlockHeader
	if err := header.FromBytes(headerBytes); err != nil {
		return nil, err
	}
	var hashStrings []string
	if record[2] != "" {
		hashStrings = strings.Split(record[2], ":")
	}
	if len(hashStrings) != int(header.FreshStake) {
		return nil, fmt.Errorf("%d ticket hashes in CSV for %d new tickets",
			len(hashStrings), header.FreshStake)
	}
	ticketHashes := make([]chainhash.Hash, 0, len(hashStrings))
	for _, hashString := range hashStrings {
		hash, err := chainhash.NewHashFromStr(hashString)
		if err != nil {
			return nil, err
		}
		ticketHashes = append(ticketHashes, *hash)
	}

	return &simData{
		header:       headerBytes,
		voters:       header.Voters,
		prevValid:    dcrutil.IsFlagSet16(header.VoteBits, dcrutil.BlockValid),
		newTickets:   header.FreshStake,
		ticketHashes: ticketHashes,
		revocations:  uint16(header.Revocations),
	}, nil
}

// reportProgress periodically prints out the current simulator height to
// stdout.
func (s *simulator) reportProgress() {
	if s.tip.height%10000 == 0 && s.tip.height != 0 {
		fmt.Println()
	}
	if s.tip.height%1000 == 0 && s.tip.height != 0 {
		fmt.Printf("..%d", s.tip.height)
	}
}

// simulateFromCSV runs the simulation using input data from a CSV file.  It is
// realistically only intended to be used with data extracted from mainnet in
// order to exactly replicate its live ticket pool.
func (s *simulator) simulateFromCSV(csvPath string) error {
	// Open the simulation CSV data which is expected to be in the following
	// format:
	//
	// Block Header,Winning Ticket Hashes
	csvFile, err := os.Open(csvPath)
	if err != nil {
		return err
	}

	// Create a new simulator using input from the CSV file.
	r := csv.NewReader(csvFile)
	r.FieldsPerRecord = fieldsPerRecord
	var handledHeader bool
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Skip header fields if they exist.
		if !handledHeader {
			handledHeader = true
			_, err := strconv.Atoi(record[0])
			if err != nil {
				continue
			}
		}

		// Convert the CSV to concrete data.
		data, err := convertRecord(record)
		if err != nil {
			return err
		}

		// Create a new node that extends the current tip using the
		// simulation data and potentially report the progress.
		s.nextNode(data)
		s.reportProgress()
	}

	return nil
}


	
	//calcDemand 1500*(avgP/nextP)^1.6
func (s *simulator) calcDemand(averagePrice float64, nextTicketPrice int64) float64 {
    

	yield := float64(averagePrice) / float64(nextTicketPrice)

	yieldTickets := 1500 * math.Pow(float64(yield), 1.6)

	if (yieldTickets / 2880) > 1 {
		return 1.0
	}
	return 1 * (yieldTickets / 2880)


}

// calcVWAP calculates and return the volume-weighted average ticket purchase
// price for up to 'StakeDiffWindows' worth of the previous ticket price
// windows.
func (s *simulator) calcPrevVWAP(prevNode *blockNode) int64 {
	windowSize := int32(s.params.StakeDiffWindowSize)
	stakeDiffWindows := int32(s.params.StakeDiffWindows)

	// Calculate the height the block just before the most recent ticket
	// price change.
	wantHeight := prevNode.height - (prevNode.height+1)%windowSize
	prevNode = s.ancestorNode(prevNode, wantHeight, nil)

	// Loop through previous required number of previous blocks and tally up
	// all of the weighted ticket purchase prices as well as the total
	// number of purchased tickets.
	numTickets, weightedVal := new(big.Int), new(big.Int)
	weightedSum, totalTickets := new(big.Int), new(big.Int)
	blocksToIterate := stakeDiffWindows * windowSize
	for i := int32(0); i < blocksToIterate && prevNode != nil; i++ {
		// weightedSum += numTickets*ticketPrice
		// totalTickets += numTickets
		numTickets.SetInt64(int64(len(prevNode.ticketsAdded)))
		weightedVal.SetInt64(prevNode.ticketPrice)
		weightedVal.Mul(weightedVal, numTickets)
		weightedSum.Add(weightedSum, weightedVal)
		totalTickets.Add(totalTickets, numTickets)
		prevNode = prevNode.parent
	}

	return new(big.Int).Div(weightedSum, totalTickets).Int64()
}

// simulate runs the simulation using a calculated demand curve which models
// how ticket purchasing would typically proceed based upon the price and the
// VWAP.
func (s *simulator) simulate(numBlocks uint64) error {
	// Shorter versions of some params for convenience.
	ticketMaturity := int32(s.params.TicketMaturity)
	ticketsPerBlock := s.params.TicketsPerBlock
	stakeValidationHeight := int32(s.params.StakeValidationHeight)
	stakeDiffWindowSize := int32(s.params.StakeDiffWindowSize)
	maxNewTicketsPerBlock := int32(s.params.MaxFreshStakePerBlock)
	maxTicketsPerWindow := maxNewTicketsPerBlock * stakeDiffWindowSize

	
	demandPerWindow := maxTicketsPerWindow
	for i := uint64(0); i < numBlocks; i++ {
		var nextHeight int32
		if s.tip != nil {
			nextHeight = s.tip.height + 1
		}

		// Purchase tickets according to simulated demand curve.
		//
		// When the height is prior to the stake validation height, just
		// use a 50% demand rate to ramp up the simulation.
		var newTickets uint8
		if nextHeight < stakeValidationHeight {
			if nextHeight >= ticketMaturity+1 {
				newTickets = uint8(maxNewTicketsPerBlock / 2)
			}
		} else {
			nextTicketPrice := s.nextTicketPriceFunc()
			if nextHeight%stakeDiffWindowSize == 0 {
				stakedCoins := s.totalSupply - s.spendableSupply
			    averagePrice := float64(stakedCoins) / float64(s.tip.poolSize)
				
				demand := s.calcDemand(averagePrice, nextTicketPrice)
				demandPerWindow = int32(float64(maxTicketsPerWindow) * demand)
			}

			newTickets = uint8(demandPerWindow / stakeDiffWindowSize)
			maxPossible := int64(s.spendableSupply) / nextTicketPrice
			if int64(newTickets) > maxPossible {
				newTickets = uint8(maxPossible)
			}
		}

		// TODO(davec): Account for tickets being purchased.
		// Limit the total staked coins to 40% of the total supply.
		stakedCoins := s.totalSupply - s.spendableSupply
		if newTickets > 0 && stakedCoins > (s.totalSupply*4/10) {
			newTickets = 0
		}

		// Start voting once stake validation height is reached.  This
		// assumes no votes are missed and revokes all expired tickets
		// as soon as possible which isn't very realistic, but it
		// doesn't have any effect on the ticket prices, so it's good
		// enough.  It could be useful to make this more realistic for
		// other simulation purposes though.
		var numVotes uint16
		if nextHeight >= stakeValidationHeight {
			numVotes = ticketsPerBlock
		}
		data := &simData{
			newTickets:  newTickets,
			prevValid:   true,
			revocations: uint16(len(s.unrevokedTickets)),
			voters:      numVotes,
		}

		// Create a new node that extends the current tip using the
		// simulation data and potentially report the progress.
		s.nextNode(data)
		s.reportProgress()
	}

	return nil
}

func main() {
	var cpuProfilePath = flag.String("cpuprofile", "",
		"Write CPU profile to the specified file")
	var csvPath = flag.String("inputcsv", "",
		"Path to simulation CSV input data -- This overrides numblocks")
	var numBlocks = flag.Uint64("numblocks", 100000, "Number of blocks to simulate")
	flag.Parse()

	// Generate a CPU profile if requested.
	if *cpuProfilePath != "" {
		f, err := os.Create(*cpuProfilePath)
		if err != nil {
			fmt.Println("Unable to create cpu profile:", err)
			return
		}
		pprof.StartCPUProfile(f)
		defer f.Close()
		defer pprof.StopCPUProfile()
	}

	// *********************************************************************
	// NOTE: Set a different function to calculate the next required stake
	// difficulty (aka ticket price) here.
	// *********************************************************************
	sim := newSimulator(&chaincfg.MainNetParams)
	sim.nextTicketPriceFunc = sim.curCalcNextStakeDiff

	startTime := time.Now()
	if *csvPath != "" {
		fmt.Printf("Running simulation from %q.\n", *csvPath)
		fmt.Printf("Height")
		if err := sim.simulateFromCSV(*csvPath); err != nil {
			fmt.Println(err)
			return
		}
	} else {
		fmt.Printf("Running simulation for %d blocks.\n", *numBlocks)
		fmt.Printf("Height")
		if err := sim.simulate(*numBlocks); err != nil {
			fmt.Println(err)
			return
		}
	}
	fmt.Println("..done")
	fmt.Println("Simulation took", time.Since(startTime))

	// Generate the simulation results and open them in a browser.
	if err := generateResults(sim); err != nil {
		fmt.Println(err)
		return
	}
}
