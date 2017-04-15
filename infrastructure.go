// Copyright (c) 2017 Dave Collins
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"html/template"
	"math"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/davecgh/dcrstakesim/internal/tickettreap"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrutil"
)

var (
	// hash256prngSeedConst is a constant derived from the hex
	// representation of pi and is used in conjuction with a caller-provided
	// seed when initializing the deterministic lottery prng.
	hash256prngSeedConst = []byte{0x24, 0x3f, 0x6a, 0x88, 0x85, 0xa3, 0x08,
		0xd3}
)

func init() {
	rand.Seed(time.Now().Unix())
}

// openBrowser tries to open the provided URL in a browser and reports whether
// or not it succeeded.
func openBrowser(url string) bool {
	var cmds [][]string
	if exe := os.Getenv("BROWSER"); exe != "" {
		cmds = append(cmds, []string{exe})
	}
	switch runtime.GOOS {
	case "darwin":
		cmds = append(cmds, []string{"/usr/bin/open"})
	case "windows":
		cmds = append(cmds, []string{"cmd", "/c", "start"})
	default:
		cmds = append(cmds, []string{"xdg-open"})
	}
	cmds = append(cmds, []string{"chrome"}, []string{"google-chrome"},
		[]string{"firefox"})

	for _, args := range cmds {
		cmd := exec.Command(args[0], append(args[1:], url)...)
		if cmd.Start() == nil {
			return true
		}
	}
	return false
}

// hash256prng is a determinstic pseudorandom number generator that uses a
// 256-bit secure hashing function to generate random uint32s starting from
// an initial seed.
type hash256prng struct {
	seed       chainhash.Hash // Initialization seed
	idx        uint64         // Hash iterator index
	cachedHash chainhash.Hash // Most recently generated hash
	hashOffset int            // Offset into most recently generated hash
}

// newHash256PRNG creates a pointer to a newly created hash256PRNG.
func newHash256PRNG(seed []byte) *hash256prng {
	// The provided seed is initialized by appending a constant derived from
	// the hex representation of pi and hashing the result to give 32 bytes.
	// This ensures the PRNG is always doing a short number of rounds
	// regardless of input since it will only need to hash small messages
	// (less than 64 bytes).
	seedHash := chainhash.HashFunc(append(seed, hash256prngSeedConst...))
	return &hash256prng{
		seed:       seedHash,
		idx:        0,
		cachedHash: seedHash,
	}
}

// Hash256Rand returns a uint32 random number using the pseudorandom number
// generator and updates the state.
func (hp *hash256prng) Hash256Rand() uint32 {
	offset := hp.hashOffset * 4
	r := binary.BigEndian.Uint32(hp.cachedHash[offset : offset+4])
	hp.hashOffset++

	// Generate a new hash and reset the hash position index once it would
	// overflow the available bytes in the most recently generated hash.
	if hp.hashOffset > 7 {
		// Hash of the seed concatenated with the hash iterator index.
		//   hash(hp.seed || hp.idx)
		data := make([]byte, len(hp.seed)+4)
		copy(data, hp.seed[:])
		binary.BigEndian.PutUint32(data[len(hp.seed):], uint32(hp.idx))
		hp.cachedHash = chainhash.HashH(data)
		hp.idx++
		hp.hashOffset = 0
	}

	// Roll over the entire PRNG by re-hashing the seed when the hash
	// iterator index overlows a uint32.
	if hp.idx > math.MaxUint32 {
		hp.seed = chainhash.HashH(hp.seed[:])
		hp.cachedHash = hp.seed
		hp.idx = 0
	}

	return r
}

// uniformRandom returns a random in the range [0, upperBound) while avoiding
// modulo bias to ensure a normal distribution within the specified range.
func (hp *hash256prng) uniformRandom(upperBound uint32) uint32 {
	if upperBound < 2 {
		return 0
	}

	// (2^32 - (x*2)) % x == 2^32 % x when x <= 2^31
	min := ((math.MaxUint32 - (upperBound * 2)) + 1) % upperBound
	if upperBound > 0x80000000 {
		min = 1 + ^upperBound
	}

	r := hp.Hash256Rand()
	for r < min {
		r = hp.Hash256Rand()
	}
	return r % upperBound
}

// stakeTicket represents a simulated sstx (stake ticket) along with the height
// of the block it was simulated to be mined in and the height it wins in.
type stakeTicket struct {
	hash        chainhash.Hash
	blockHeight int32
	price       dcrutil.Amount
	winHeight   int32
}

// newStakeTicket returns a new simulated stake ticket with the given hash and
// purchased at the provided height.
func newStakeTicket(hash *chainhash.Hash, purchaseHeight int32, price int64) *stakeTicket {
	return &stakeTicket{
		hash:        *hash,
		blockHeight: purchaseHeight,
		price:       dcrutil.Amount(price),
		winHeight:   -1,
	}
}

// stakeTicketHash generates a fake, but deterministic, stake ticket hash based
// on the height the ticket is purchased at as well as its position within the
// simulated block.
func stakeTicketHash(purchaseHeight int32, ticketNum uint8) chainhash.Hash {
	var b [8]byte
	binary.LittleEndian.PutUint32(b[:], uint32(purchaseHeight))
	binary.LittleEndian.PutUint32(b[4:], uint32(ticketNum))
	return chainhash.HashH(b[:])
}

// uint32Sorter implements sort.Interface to allow a slice of 32-bit unsigned
// integers to be sorted.
type uint32Sorter []uint32

// Len returns the number of 32-bit unsigned integers in the slice.  It is part
// of the sort.Interface implementation.
func (s uint32Sorter) Len() int {
	return len(s)
}

// Swap swaps the 32-bit unsigned integers at the passed indices.  It is part of
// the sort.Interface implementation.
func (s uint32Sorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Less returns whether the 64-bit integer with index i should sort before the
// 32-bit unsigned integer with index j.  It is part of the sort.Interface
// implementation.
func (s uint32Sorter) Less(i, j int) bool {
	return s[i] < s[j]
}

// winningTickets returns a slice of tickets that are required to vote for the
// given block being voted on and current live ticket pool.
func winningTickets(voteBlock *blockNode, liveTickets *tickettreap.Immutable, numVotes uint16) ([]*stakeTicket, error) {
	// Ensure the number of live tickets is within the allowable range.
	numLiveTickets := uint32(liveTickets.Len())
	if numLiveTickets > math.MaxUint32 {
		return nil, fmt.Errorf("live ticket pool has %d tickets which "+
			"is more than the max allowed of %d", numLiveTickets,
			math.MaxUint32)
	}
	if uint32(numVotes) > numLiveTickets {
		return nil, fmt.Errorf("live ticket pool has %d tickets, "+
			"while %d are needed to vote", numLiveTickets, numVotes)
	}

	// Construct list of winners by generating successive values from the
	// deterministic prng and using them as indices into the sorted live
	// ticket pool while skipping any duplicates that might occur.
	prng := newHash256PRNG(voteBlock.header)
	winningOffsets := make([]uint32, 0, numVotes)
	usedOffsets := make(map[uint32]struct{})
	for uint16(len(winningOffsets)) < numVotes {
		ticketIndex := prng.uniformRandom(numLiveTickets)
		if _, exists := usedOffsets[ticketIndex]; !exists {
			usedOffsets[ticketIndex] = struct{}{}
			winningOffsets = append(winningOffsets, ticketIndex)
		}
	}
	sort.Sort(uint32Sorter(winningOffsets))

	// Reconstruct the winning stake tickets based upon the winning indices.
	winners := make([]*stakeTicket, 0, numVotes)
	var poolIdx, winnerIdx uint32
	liveTickets.ForEach(func(key tickettreap.Key, val *tickettreap.Value) bool {
		if poolIdx == winningOffsets[winnerIdx] {
			ticketHash := (*chainhash.Hash)(&key)
			ticket := newStakeTicket(ticketHash, val.PurchaseHeight,
				val.PurchasePrice)
			ticket.winHeight = voteBlock.height + 1
			winners = append(winners, ticket)
			if uint16(len(winners)) == numVotes {
				return false
			}
			winnerIdx++
		}
		poolIdx++
		return true
	})

	return winners, nil
}

// blockNode represent a block in the simulated chain along with additional data
// about the block calculated during the simulation.
type blockNode struct {
	parent *blockNode
	height int32
	header []byte
	next   *blockNode

	ticketPrice     int64          // Stake difficulty target.
	regularSubsidy  dcrutil.Amount // PoW and dev subsidies of this block.
	poolSize        uint32         // Total pool size as of this block.
	totalSupply     dcrutil.Amount // Total supply as of this block.
	spendableSupply dcrutil.Amount // Spendable supply as of this block.

	// stakedCoins is the amount of coins that are staked as of this block.
	// It does not consider maturity periods since even though coins that
	// have recently become unlocked aren't spendable until they mature,
	// those coins are not staked.
	stakedCoins dcrutil.Amount

	numVoters      uint16
	ticketsAdded   []*stakeTicket
	ticketsVoted   []*stakeTicket
	ticketsRevoked []*stakeTicket
}

// newBlockNode returns a new simulated block node the is connected to the
// provided parent node and is populated with the provided params.
func newBlockNode(parent *blockNode, ticketsAdded, ticketsVoted, ticketsRevoked []*stakeTicket) *blockNode {
	node := &blockNode{
		parent:         parent,
		ticketsAdded:   ticketsAdded,
		ticketsVoted:   ticketsVoted,
		ticketsRevoked: ticketsRevoked,
	}
	if parent != nil {
		parent.next = node
		node.height = parent.height + 1
	}
	return node
}

// simulator provides a proof-of-stake simulation framework for Decred which
// includes a live ticket pool that works with ticket maturity, purchasing,
// revocation, expiration, and winning ticket selection along with ticket price
// calculation.  It also provides some other features such as coin supply
// calculation.
type simulator struct {
	params  *chaincfg.Params
	verbose bool

	// The fields are related to the simulated chain.
	root *blockNode
	tip  *blockNode

	// These fields are related to tracking tickets as they enter and exit
	// the live ticket pool due to events such as maturing, winning the
	// lottery, failing to vote, and expiring.
	immatureTickets  []*stakeTicket
	liveTickets      *tickettreap.Immutable
	expireHeights    map[int32][]*stakeTicket
	expiredTickets   []*stakeTicket
	missedTickets    []*stakeTicket
	unrevokedTickets []*stakeTicket
	wonTickets       []*stakeTicket

	// maturingSupply keeps track of how much coin supply will mature at
	// each height.
	maturingSupply map[int32]dcrutil.Amount

	// These fields control the ticket price and demand distribution
	// functions used in the simulation.  The demand func takes the next
	// height and the ticket price produced by the next ticket price func.
	nextTicketPriceFunc func() int64
	demandFunc          func(int32, int64) float64
}

// calcFullSubsidy returns the full block subsidy for the given block height.
func (s *simulator) calcFullSubsidy(blockHeight int32) dcrutil.Amount {
	iterations := int64(blockHeight) / s.params.SubsidyReductionInterval
	subsidy := s.params.BaseSubsidy
	for i := int64(0); i < iterations; i++ {
		subsidy *= s.params.MulSubsidy
		subsidy /= s.params.DivSubsidy
	}
	return dcrutil.Amount(subsidy)
}

// calcPoWSubsidy returns the proof-of-work subsidy portion from a given full
// subsidy, block height, and number of votes that will be included in the
// block.
func (s *simulator) calcPoWSubsidy(fullSubsidy dcrutil.Amount, blockHeight int32, numVotes uint16) dcrutil.Amount {
	powProportion := dcrutil.Amount(s.params.WorkRewardProportion)
	totalProportions := dcrutil.Amount(s.params.TotalSubsidyProportions())
	powSubsidy := (fullSubsidy * powProportion) / totalProportions
	if int64(blockHeight) < s.params.StakeValidationHeight {
		return powSubsidy
	}

	// Reduce the subsidy according to the number of votes.
	ticketsPerBlock := dcrutil.Amount(s.params.TicketsPerBlock)
	return (powSubsidy * dcrutil.Amount(numVotes)) / ticketsPerBlock
}

// calcPoSSubsidy returns the proof-of-stake subsidy portion for a given block
// height being voted on.
func (s *simulator) calcPoSSubsidy(heightVotedOn int32) dcrutil.Amount {
	if int64(heightVotedOn+1) < s.params.StakeValidationHeight {
		return 0
	}

	fullSubsidy := s.calcFullSubsidy(heightVotedOn)
	posProportion := dcrutil.Amount(s.params.StakeRewardProportion)
	totalProportions := dcrutil.Amount(s.params.TotalSubsidyProportions())
	return (fullSubsidy * posProportion) / totalProportions
}

// calcDevSubsidy returns the dev org subsidy portion from a given full subsidy.
func (s *simulator) calcDevSubsidy(fullSubsidy dcrutil.Amount, blockHeight int32, numVotes uint16) dcrutil.Amount {
	devProportion := dcrutil.Amount(s.params.BlockTaxProportion)
	totalProportions := dcrutil.Amount(s.params.TotalSubsidyProportions())
	devSubsidy := (fullSubsidy * devProportion) / totalProportions
	if int64(blockHeight) < s.params.StakeValidationHeight {
		return devSubsidy
	}

	// Reduce the subsidy according to the number of votes.
	ticketsPerBlock := dcrutil.Amount(s.params.TicketsPerBlock)
	return (devSubsidy * dcrutil.Amount(numVotes)) / ticketsPerBlock
}

// ancestorNode returns the ancestor node at the provided height by following
// the chain backwards from the given node.  The returned node will be nil when
// a height is requested that is after the height of the passed node.  Also, a
// callback can optionally be provided that is invoked with each node as it
// traverses.
func (s *simulator) ancestorNode(node *blockNode, height int32, f func(*blockNode)) *blockNode {
	// Nothing to do if the requested height is outside of the valid
	// range.
	if node == nil || height > node.height {
		return nil
	}

	// Iterate backwards until the requested height is reached.
	for node != nil && node.height > height {
		node = node.parent
		if f != nil && node != nil {
			f(node)
		}
	}

	return node
}

// mergeDifficulty takes an original stake difficulty and two new, scaled
// stake difficulties, merges the new difficulties, and outputs a new
// merged stake difficulty.
func mergeDifficulty(oldDiff int64, newDiff1 int64, newDiff2 int64) int64 {
	newDiff1Big := big.NewInt(newDiff1)
	newDiff2Big := big.NewInt(newDiff2)
	newDiff2Big.Lsh(newDiff2Big, 32)

	oldDiffBig := big.NewInt(oldDiff)
	oldDiffBigLSH := big.NewInt(oldDiff)
	oldDiffBigLSH.Lsh(oldDiffBig, 32)

	newDiff1Big.Div(oldDiffBigLSH, newDiff1Big)
	newDiff2Big.Div(newDiff2Big, oldDiffBig)

	// Combine the two changes in difficulty.
	summedChange := big.NewInt(0)
	summedChange.Set(newDiff2Big)
	summedChange.Lsh(summedChange, 32)
	summedChange.Div(summedChange, newDiff1Big)
	summedChange.Mul(summedChange, oldDiffBig)
	summedChange.Rsh(summedChange, 32)

	return summedChange.Int64()
}

// limitRetarget clamps the passed new difficulty to the old one adjusted by the
// factor specified in the chain parameters.  This ensures the difficulty can
// only move up or down by a limited amount.
func (s *simulator) limitRetarget(oldDiff, newDiff int64) int64 {
	maxRetarget := s.params.RetargetAdjustmentFactor
	switch {
	case newDiff == 0:
		fallthrough
	case (oldDiff / newDiff) > (maxRetarget - 1):
		return oldDiff / maxRetarget
	case (newDiff / oldDiff) > (maxRetarget - 1):
		return oldDiff * maxRetarget
	}

	return newDiff
}

// curCalcNextStakeDiff returns the required stake difficulty (aka ticket price)
// for the block after the current tip block the generator is associated with
// using the current algorithm deployed on mainnet as of Mar 2017.
//
// An overview of the algorithm is as follows:
// 1) Use the minimum value for any blocks before any tickets could have
//    possibly been purchased due to coinbase maturity requirements
// 2) Return 0 if the current tip block stake difficulty is 0.  This is a
//    safety check against a condition that should never actually happen.
// 3) Use the previous block's difficulty if the next block is not at a retarget
//    interval
// 4) Calculate the ideal retarget difficulty for each window based on the
//    actual pool size in the window versus the target pool size skewed by a
//    constant factor to weight the ticket pool size instead of the tickets per
//    block and exponentially weight each difficulty such that the most recent
//    window has the highest weight
// 5) Calculate the pool size retarget difficulty based on the exponential
//    weighted average and ensure it is limited to the max retarget adjustment
//    factor -- This is the first metric used to calculate the final difficulty
// 6) Calculate the ideal retarget difficulty for each window based on the
//    actual new tickets in the window versus the target new tickets per window
//    and exponentially weight each difficulty such that the most recent window
//    has the highest weight
// 7) Calculate the tickets per window retarget difficulty based on the
//    exponential weighted average and ensure it is limited to the max retarget
//    adjustment factor
// 8) Calculate the final difficulty by averaging the pool size retarget
//    difficulty from #5 and the tickets per window retarget difficulty from #7
//    using scaled multiplication and ensure it is limited to the max retarget
//    adjustment factor
func (s *simulator) curCalcNextStakeDiff() int64 {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int32(0)
	if s.tip != nil {
		nextHeight = s.tip.height + 1
	}
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return s.params.MinimumStakeDiff
	}

	// Return 0 if the current difficulty is already zero since any scaling
	// of 0 is still 0.  This should never really happen since there is a
	// minimum stake difficulty, but the consensus code checks the condition
	// just in case, so follow suit here.
	curDiff := s.tip.ticketPrice
	if curDiff == 0 {
		return 0
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	windowSize := s.params.StakeDiffWindowSize
	if int64(nextHeight)%windowSize != 0 {
		return curDiff
	}

	// --------------------------------
	// Ideal pool size retarget metric.
	// --------------------------------

	// Calculate the ideal retarget difficulty for each window based on the
	// actual pool size in the window versus the target pool size and
	// exponentially weight them.
	var weightSum int64
	adjusted, weightedPoolSizeSum := new(big.Int), new(big.Int)
	ticketsPerBlock := int64(s.params.TicketsPerBlock)
	targetPoolSize := ticketsPerBlock * int64(s.params.TicketPoolSize)
	targetpoolSizeBig := big.NewInt(targetPoolSize)
	numWindows := s.params.StakeDiffWindows
	weightAlpha := s.params.StakeDiffAlpha
	node := s.tip
	for i := int64(0); i < numWindows; i++ {
		// Get the pool size for the block at the start of the window.
		// Use zero if there are not yet enough blocks left to cover the
		// window.
		prevRetargetHeight := nextHeight - int32(windowSize*(i+1))
		windowPoolSize := int64(0)
		node = s.ancestorNode(node, prevRetargetHeight, nil)
		if node != nil {
			windowPoolSize = int64(node.poolSize)
		}

		// Skew the pool size by the constant weight factor specified in
		// the chain parameters (which is typically the max adjustment
		// factor) in order to help weight the ticket pool size versus
		// tickets per block.  Also, ensure the skewed pool size is a
		// minimum of 1.
		skewedPoolSize := targetPoolSize + (windowPoolSize-
			targetPoolSize)*int64(s.params.TicketPoolSizeWeight)
		if skewedPoolSize <= 0 {
			skewedPoolSize = 1
		}

		// Calculate the ideal retarget difficulty for the window based
		// on the skewed pool size and weight it exponentially by
		// multiplying it by 2^(window_number) such that the most recent
		// window receives the most weight.
		//
		// Also, since integer division is being used, shift up the
		// number of new tickets 32 bits to avoid losing precision.
		//
		//   adjusted = (skewedPoolSize << 32) / targetPoolSize
		//   adjusted = adjusted << (numWindows-i)*weightAlpha
		//   weightedPoolSizeSum += adjusted
		//
		adjusted.SetInt64(skewedPoolSize)
		adjusted.Lsh(adjusted, 32)
		adjusted.Div(adjusted, targetpoolSizeBig)
		adjusted.Lsh(adjusted, uint((numWindows-i)*weightAlpha))
		weightedPoolSizeSum.Add(weightedPoolSizeSum, adjusted)
		weightSum += 1 << uint64((numWindows-i)*weightAlpha)
	}

	// Calculate the pool size retarget difficulty based on the exponential
	// weighted average and shift the result back down 32 bits to account
	// for the previous shift up in order to avoid losing precision.  Then,
	// limit it to the maximum allowed retarget adjustment factor.
	//
	// This is the first metric used in the final calculated difficulty.
	//
	//  nextPoolSizeDiff = ((weightedPoolSizeSum/weightSum) * curDiff) >> 32
	//
	curDiffBig := big.NewInt(curDiff)
	weightSumBig := big.NewInt(weightSum)
	weightedPoolSizeSum.Div(weightedPoolSizeSum, weightSumBig)
	weightedPoolSizeSum.Mul(weightedPoolSizeSum, curDiffBig)
	weightedPoolSizeSum.Rsh(weightedPoolSizeSum, 32)
	nextPoolSizeDiff := weightedPoolSizeSum.Int64()
	nextPoolSizeDiff = s.limitRetarget(curDiff, nextPoolSizeDiff)

	// -----------------------------------------
	// Ideal tickets per window retarget metric.
	// -----------------------------------------

	// Calculate the ideal retarget difficulty for each window based on the
	// actual number of new tickets in the window versus the target tickets
	// per window and exponentially weight them.
	weightedTicketsSum := big.NewInt(0)
	targetTicketsPerWindow := ticketsPerBlock * windowSize
	targetTicketsPerWindowBig := big.NewInt(targetTicketsPerWindow)
	node = s.tip
	for i := int64(0); i < numWindows; i++ {
		// Since the difficulty for the next block after the current tip
		// is being calculated and there is no such block yet, the sum
		// of all new tickets in the first window needs to start with
		// the number of new tickets in the tip block.
		var windowNewTickets int64
		if i == 0 {
			windowNewTickets = int64(len(node.ticketsAdded))
		}

		// Tally all of the new tickets in all blocks in the window and
		// ensure the number of new tickets is a minimum of 1.
		prevRetargetHeight := nextHeight - int32(windowSize*(i+1))
		node = s.ancestorNode(node, prevRetargetHeight, func(node *blockNode) {
			windowNewTickets += int64(len(node.ticketsAdded))
		})
		if windowNewTickets <= 0 {
			windowNewTickets = 1
		}

		// Calculate the ideal retarget difficulty for the window based
		// on the number of new tickets and weight it exponentially by
		// multiplying it by 2^(window_number) such that the most recent
		// window receives the most weight.
		//
		// Also, since integer division is being used, shift up the
		// number of new tickets 32 bits to avoid losing precision.
		//
		//  adjusted = (windowNewTickets << 32) / targetTicketsPerWindow
		//  adjusted = adjusted << (numWindows-i)*weightAlpha
		//  weightedTicketsSum += adjusted
		//
		adjusted.SetInt64(windowNewTickets)
		adjusted.Lsh(adjusted, 32)
		adjusted.Div(adjusted, targetTicketsPerWindowBig)
		adjusted.Lsh(adjusted, uint((numWindows-i)*weightAlpha))
		weightedTicketsSum.Add(weightedTicketsSum, adjusted)
	}

	// Calculate the tickets per window retarget difficulty based on the
	// exponential weighted average and shift the result back down 32 bits
	// to account for the previous shift up in order to avoid losing
	// precision.  Then, limit it to the maximum allowed retarget adjustment
	// factor.
	//
	// This is the second metric used in the final calculated difficulty.
	//
	// nextNewTixDiff = ((weightedTicketsSum/weightSum) * curDiff) >> 32
	//
	weightedTicketsSum.Div(weightedTicketsSum, weightSumBig)
	weightedTicketsSum.Mul(weightedTicketsSum, curDiffBig)
	weightedTicketsSum.Rsh(weightedTicketsSum, 32)
	nextNewTixDiff := weightedTicketsSum.Int64()
	nextNewTixDiff = s.limitRetarget(curDiff, nextNewTixDiff)

	// Average the previous two metrics using scaled multiplication and
	// ensure the result is limited to both the maximum allowed retarget
	// adjustment factor and the minimum allowed stake difficulty.
	nextDiff := mergeDifficulty(curDiff, nextPoolSizeDiff, nextNewTixDiff)
	nextDiff = s.limitRetarget(curDiff, nextDiff)
	if nextDiff < s.params.MinimumStakeDiff {
		return s.params.MinimumStakeDiff
	}
	return nextDiff
}

// removeTicket removes the passed index from the provided slice of tickets and
// returns the resulting slice.  This is an in-place modification.
func removeTicket(tickets []*stakeTicket, index int) []*stakeTicket {
	copy(tickets[index:], tickets[index+1:])
	tickets[len(tickets)-1] = nil // Prevent memory leak
	tickets = tickets[:len(tickets)-1]
	return tickets
}

// connectLiveTickets updates the live ticket pool for a new tip block by
// removing the provided winners and the tickets that are now expired and adding
// any immature tickets which are now mature.
func (s *simulator) connectLiveTickets(height int32, winners, purchases []*stakeTicket) {
	// Move winning tickets from the live ticket pool to won tickets pool.
	for _, winner := range winners {
		s.liveTickets = s.liveTickets.Delete(tickettreap.Key(winner.hash))
		s.wonTickets = append(s.wonTickets, winner)
	}

	// Move expired tickets from the live ticket pool to the expired and
	// unrevoked ticket pools.
	tickets := s.expireHeights[height]
	for _, ticket := range tickets {
		if s.liveTickets.Has(tickettreap.Key(ticket.hash)) {
			s.expiredTickets = append(s.expiredTickets, ticket)
			s.unrevokedTickets = append(s.unrevokedTickets, ticket)
		}
		s.liveTickets = s.liveTickets.Delete(tickettreap.Key(ticket.hash))
	}
	delete(s.expireHeights, height)

	// Move immature tickets which are now mature to the live ticket pool.
	ticketMaturity := int32(s.params.TicketMaturity)
	for i := 0; i < len(s.immatureTickets); i++ {
		ticket := s.immatureTickets[i]
		liveHeight := ticket.blockHeight + ticketMaturity
		if height >= liveHeight {
			s.immatureTickets = removeTicket(s.immatureTickets, i)
			s.liveTickets = s.liveTickets.Put(
				tickettreap.Key(ticket.hash),
				&tickettreap.Value{
					PurchaseHeight: ticket.blockHeight,
					PurchasePrice:  int64(ticket.price),
				})

			// This is required because the ticket at the current
			// offset was just removed from the slice that is being
			// iterated, so adjust the offset down one accordingly.
			i--
		}
	}

	// Add new ticket purchases to the immature ticket pool.
	s.immatureTickets = append(s.immatureTickets, purchases...)

	// Add new ticket purchases to the map which tracks when they expire.
	liveHeight := height + int32(s.params.TicketMaturity) + 1
	expireHeight := liveHeight + int32(s.params.TicketExpiry) - 1
	s.expireHeights[expireHeight] = purchases
}

// simData houses information used to drive the simulation.
//
// The fields that are marked optional will be automatically generated if not
// provided.  They are primarily useful since they allow the simulation to use
// live data from mainnet to create a exact replication of its ticket pool.
type simData struct {
	header       []byte // Optional
	voters       uint16
	prevValid    bool
	newTickets   uint8
	ticketHashes []chainhash.Hash // Optional
	revocations  uint16
}

// nextNode generates a node the builds from the current simulator tip using the
// passed data to obtain the specific details such as the number of new tickets
// to purchase, how many tickets to revoke, and the number of voters and makes
// it the new tip.
//
// It also includes sanity checking on the input data and performs various
// bookkeeping such as tracking the live ticket pool, winning tickets, subsidy
// generation per number of voters in the input data, and total coin supply.
func (s *simulator) nextNode(data *simData) *blockNode {
	var nextHeight int32
	var totalSupply, spendableSupply, stakedCoins dcrutil.Amount
	if s.tip != nil {
		nextHeight = s.tip.height + 1
		totalSupply = s.tip.totalSupply
		spendableSupply = s.tip.spendableSupply
		stakedCoins = s.tip.stakedCoins
	}

	// Shorter versions of some parameters for convenience.
	ticketsPerBlock := s.params.TicketsPerBlock
	stakeValidationHeight := s.params.StakeValidationHeight
	coinbaseMaturity := int32(s.params.CoinbaseMaturity)
	ticketMaturity := int32(s.params.TicketMaturity)

	// Perform a bit of sanity checking on the simulation input data.
	if data.newTickets > s.params.MaxFreshStakePerBlock {
		panic(fmt.Sprintf("Simulation data attempted to purchase "+
			"%d new tickets at height %d which is greater than "+
			"max allowed per block %d", data.newTickets, nextHeight,
			s.params.MaxFreshStakePerBlock))
	}
	if data.voters > ticketsPerBlock {
		panic(fmt.Sprintf("Simulation data attempted to include %d "+
			"votes at height %d which is greater than max allowed "+
			"per block %d", data.voters, nextHeight,
			ticketsPerBlock))
	}
	if int(data.revocations) > len(s.unrevokedTickets) {
		panic(fmt.Sprintf("Simulation data attempted to revoke %d "+
			"tickets at height %d which is greater than unrevoked "+
			"tickets %d", data.revocations, nextHeight,
			len(s.unrevokedTickets)))
	}
	if int64(nextHeight) >= stakeValidationHeight &&
		data.voters < (ticketsPerBlock/2+1) {
		panic(fmt.Sprintf("Simulation data attempted to include %d "+
			"votes at height %d which is less than min allowed "+
			"per block %d", data.voters, nextHeight,
			(ticketsPerBlock/2 + 1)))
	}
	if nextHeight <= int32(s.params.CoinbaseMaturity) {
		if data.newTickets != 0 {
			panic(fmt.Sprintf("Simulation data attempted to "+
				"purchase %d new tickets at height %d before "+
				"any coins are spendable", data.newTickets,
				nextHeight))
		}
	} else if int64(nextHeight) < stakeValidationHeight {
		if data.voters != 0 {
			panic(fmt.Sprintf("Simulation data attempted to "+
				"vote with %d tickets at height %d before "+
				"stake validation height %d", data.voters,
				nextHeight, stakeValidationHeight))
		}
		if data.revocations != 0 {
			panic(fmt.Sprintf("Simulation data attempted to "+
				"revoke %d tickets at height %d before stake "+
				"validation height %d", data.revocations,
				nextHeight, stakeValidationHeight))
		}
	}

	// Generate votes once the stake validation height has been reached.
	var ticketsWon, ticketsVoted, ticketsMissed []*stakeTicket
	if int64(nextHeight) >= stakeValidationHeight {
		winners, err := winningTickets(s.tip, s.liveTickets,
			ticketsPerBlock)
		if err != nil {
			panic(err)
		}

		ticketsWon = winners
		ticketsVoted = winners[:data.voters]
		ticketsMissed = winners[data.voters:]
	}

	// Reduce the number of staked coins by all that are becoming unlocked
	// from the tickets that have voted.
	for _, ticket := range ticketsVoted {
		stakedCoins -= ticket.price
	}

	// Update the current spendable coins supply to include the coins that
	// will mature in the new block.
	spendableSupply += s.maturingSupply[nextHeight]
	delete(s.maturingSupply, nextHeight)

	// Generate mock stake tickets for each new one purchased in the block
	// and deduct the amount from the spendable supply since the coins will
	// be locked.
	ticketPrice := s.nextTicketPriceFunc()
	var ticketsAdded []*stakeTicket
	for i := uint8(0); i < data.newTickets; i++ {
		// Don't purchase any more tickets if there aren't enough
		// spendable coins to actually purchase them.
		if spendableSupply < dcrutil.Amount(ticketPrice) {
			break
		}

		// Either use the ticket hash provided by the simulation data or
		// generate a mock hash when none are provided.
		var ticketHash chainhash.Hash
		if data.ticketHashes != nil {
			ticketHash = data.ticketHashes[i]
		} else {
			ticketHash = stakeTicketHash(nextHeight, i)
		}
		ticket := newStakeTicket(&ticketHash, nextHeight, ticketPrice)
		ticketsAdded = append(ticketsAdded, ticket)
		spendableSupply -= dcrutil.Amount(ticketPrice)
		stakedCoins += dcrutil.Amount(ticketPrice)
	}

	// Choose the simulated number of revocations from the pool of eligible
	// revocations.
	var ticketsRevoked []*stakeTicket
	for i := uint16(0); i < data.revocations; i++ {
		ticket := s.unrevokedTickets[0]
		s.unrevokedTickets = s.unrevokedTickets[1:]
		ticketsRevoked = append(ticketsRevoked, ticket)
		stakedCoins -= ticket.price
	}

	// Create a new fake block based on the provided simulation data and
	// ticket information generated above.
	node := newBlockNode(s.tip, ticketsAdded, ticketsVoted, ticketsRevoked)
	node.header = data.header
	if node.header == nil {
		// Generate fake header bytes based on the height when it wasn't
		// provided by the simulation data.
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(nextHeight))
		node.header = buf[:]
	}
	node.numVoters = data.voters
	node.ticketPrice = ticketPrice
	node.poolSize = uint32(s.liveTickets.Len())
	node.spendableSupply = spendableSupply
	node.stakedCoins = stakedCoins

	if s.verbose {
		fmt.Printf("nextHeight %v, poolsize %v, immature %v, total %v, "+
			"spendable %v, bought %v @ %v\n", nextHeight,
			node.poolSize, len(s.immatureTickets),
			node.poolSize+uint32(len(s.immatureTickets)),
			spendableSupply, len(ticketsAdded),
			dcrutil.Amount(ticketPrice))
	}

	// Calculate the total new supply generated by this block and keep a
	// running tally of the total supply.  Also, keep track of when the
	// newly generated coins will mature.
	var parentRegularSubsidy dcrutil.Amount
	if node.parent != nil {
		parentRegularSubsidy = node.parent.regularSubsidy
	}
	if nextHeight == 1 {
		node.regularSubsidy = dcrutil.Amount(s.params.BlockOneSubsidy())
		s.maturingSupply[nextHeight+coinbaseMaturity] = node.regularSubsidy
	} else if nextHeight > 0 {
		// Calculate subsidies for the new block.
		fullSubsidy := s.calcFullSubsidy(nextHeight)
		devSubsidy := s.calcDevSubsidy(fullSubsidy, nextHeight, data.voters)
		powSubsidy := s.calcPoWSubsidy(fullSubsidy, nextHeight, data.voters)
		posSubsidy := s.calcPoSSubsidy(nextHeight - 1)
		perVoteSubsidy := posSubsidy / dcrutil.Amount(ticketsPerBlock)
		voteSubsidy := perVoteSubsidy * dcrutil.Amount(data.voters)

		// The current model is to only add the proof-of-work and dev
		// subsidy generated by the previous block to the total supply
		// if it wasn't invalidated.  This means the reported total
		// supply is always one block behind what is actually available.
		if !data.prevValid {
			parentRegularSubsidy = 0
		}
		newSupply := parentRegularSubsidy + voteSubsidy

		node.regularSubsidy = powSubsidy + devSubsidy
		node.totalSupply = totalSupply + newSupply

		// Account for maturity of the newly generated coins from PoW,
		// PoS, and revocations.
		maturedHeight := nextHeight + coinbaseMaturity
		s.maturingSupply[maturedHeight] += node.regularSubsidy
		ticketMaturedHeight := nextHeight + ticketMaturity
		s.maturingSupply[ticketMaturedHeight] += voteSubsidy
		for _, ticket := range ticketsVoted {
			s.maturingSupply[ticketMaturedHeight] += ticket.price
		}
		for _, ticket := range ticketsRevoked {
			s.maturingSupply[ticketMaturedHeight] += ticket.price
		}
	}

	// Update the live ticket pool by adding the newly purchased tickets,
	// removing the winning tickets, removing any tickets that are now
	// expired, and update related state.  Also, add missed tickets to the
	// unrevoked tickets pool.
	s.unrevokedTickets = append(s.unrevokedTickets, ticketsMissed...)
	s.connectLiveTickets(nextHeight, ticketsWon, ticketsAdded)
	s.tip = node
	if s.root == nil {
		s.root = node
	}
	return node
}

// newSimulator returns an instance of a type that can be used to perform
// proof-of-stake simulations.
func newSimulator(params *chaincfg.Params, verbose bool) *simulator {
	return &simulator{
		params:         params,
		verbose:        verbose,
		liveTickets:    tickettreap.NewImmutable(),
		expireHeights:  make(map[int32][]*stakeTicket),
		maturingSupply: make(map[int32]dcrutil.Amount),
	}
}

// generateResults creates an HTML results file for a completed simulation and
// opens it using a browser.
func generateResults(s *simulator, resultsPath, proposalName, ddfName string) error {
	// Parse the results template.
	resultsTpl, err := template.New("results").Parse(resultsTmplText)
	if err != nil {
		return fmt.Errorf("unable to parse results template: %v", err)
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return fmt.Errorf("unable to create results: %v", err)
	}
	defer resultsFile.Close()

	// Shorter version of some params for convenience.
	stakeValidationHeight := int32(s.params.StakeValidationHeight)

	// Generate the data needed for the HTML template and execute it in
	// order to generate the final HTML results file.
	var poolSizeCSV, ticketPriceCSV, supplyCSV bytes.Buffer
	minTicketPrice, maxTicketPrice := int64(math.MaxInt64), int64(0)
	minPoolSize, maxPoolSize := uint32(math.MaxUint32), uint32(0)
	for node := s.root; node != nil; node = node.next {
		heightStr := strconv.Itoa(int(node.height))
		poolSizeCSV.WriteString(heightStr)
		poolSizeCSV.WriteRune(',')
		poolSizeCSV.WriteString(strconv.FormatInt(int64(node.poolSize), 10))
		poolSizeCSV.WriteRune('\n')

		if node.height%int32(s.params.StakeDiffWindowSize) == 0 {
			ticketPriceCSV.WriteString(heightStr)
			ticketPriceCSV.WriteRune(',')
			price := dcrutil.Amount(node.ticketPrice).ToCoin()
			priceStr := strconv.FormatFloat(price, 'f', 8, 64)
			ticketPriceCSV.WriteString(priceStr)
			ticketPriceCSV.WriteRune('\n')
		}

		supplyCSV.WriteString(heightStr)
		supplyCSV.WriteRune(',')
		supply := node.totalSupply.ToCoin() / 1e6
		supplyCSV.WriteString(strconv.FormatFloat(supply, 'f', 8, 64))
		supplyCSV.WriteRune(',')
		staked := node.stakedCoins.ToCoin() / 1e6
		supplyCSV.WriteString(strconv.FormatFloat(staked, 'f', 8, 64))
		supplyCSV.WriteRune('\n')

		if node.ticketPrice < minTicketPrice {
			minTicketPrice = node.ticketPrice
		}
		if node.ticketPrice > maxTicketPrice {
			maxTicketPrice = node.ticketPrice
		}

		// Only consider pool size after stake validation height unless
		// the entire simulation is before that point.
		if node.height >= stakeValidationHeight || s.tip.height < stakeValidationHeight {
			if node.poolSize < minPoolSize {
				minPoolSize = node.poolSize
			}
			if node.poolSize > maxPoolSize {
				maxPoolSize = node.poolSize
			}
		}
	}
	totalTickets := s.liveTickets.Len() + len(s.immatureTickets) +
		len(s.wonTickets) + len(s.expiredTickets)
	expiredPercent := float64(len(s.expiredTickets)) * 100 / float64(totalTickets)
	parameters := []struct {
		Name  string
		Value string
	}{
		{"Price Function", proposalName},
		{"Demand Distribution Function", ddfName},
	}
	err = resultsTpl.Execute(resultsFile, map[string]interface{}{
		"PoolSizeCSV":     poolSizeCSV.String(),
		"TicketPriceCSV":  ticketPriceCSV.String(),
		"SupplyCSV":       supplyCSV.String(),
		"MinTicketPrice":  dcrutil.Amount(minTicketPrice).String(),
		"MaxTicketPrice":  dcrutil.Amount(maxTicketPrice).String(),
		"NumTickets":      totalTickets,
		"NumWinners":      len(s.wonTickets),
		"NumExpired":      len(s.expiredTickets),
		"ExpiredPercent":  strconv.FormatFloat(expiredPercent, 'f', 2, 64),
		"MinPoolSize":     minPoolSize,
		"MaxPoolSize":     maxPoolSize,
		"CoinSupply":      s.tip.totalSupply.String(),
		"SpendableSupply": s.tip.spendableSupply.String(),
		"Parameters":      parameters,
		"SurgeUpHeight":   surgeUpHeight,
		"SurgeDownHeight": surgeDownHeight,
	})
	if err != nil {
		return fmt.Errorf("unable to execute template: %v", err)
	}

	fmt.Printf("Results path: %q\n", resultsPath)
	if !openBrowser(resultsPath) {
		return fmt.Errorf("unable to open results file %q in browser",
			resultsPath)
	}

	return nil
}
