// Copyright (c) 2017 Dave Collins
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"

	"github.com/davecgh/dcrstakesim/internal/tickettreap"
	"github.com/decred/dcrutil"
)

var s1 float64
var minStakeDiff int64

func (s *simulator) calcNextStakeDiffProposal6() int64 {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int32(0)
	if s.tip != nil {
		nextHeight = s.tip.height + 1
	}
	altMinDiff := minStakeDiff * 1e8 // normally s.params.MinimumStakeDiff
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return altMinDiff
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// The following variables are used in the algorithm
	// Pool: p, c, t (previous, current, target)
	// Price : q, curDiff, n (previous, current, next)

	// Pool
	c := int64(s.tip.poolSize) + int64(len(s.immatureTickets))
	t := int64(s.params.TicketsPerBlock) * int64(s.params.TicketPoolSize)
	// Previous window pool size and ticket price
	p, q := s.poolSizeAndDiff(s.tip.height - int32(intervalSize))
	// Return the existing ticket price for the first interval.
	if p == 0 {
		return curDiff
	}

	// Useful ticket counts are A (-5 * 144) and B (15 * 144)
	//A := -int64(s.params.TicketsPerBlock) * intervalSize
	B := (int64(s.params.MaxFreshStakePerBlock) - int64(s.params.TicketsPerBlock)) * intervalSize
	t += 1280 // not B (1440)?

	// Pool velocity
	//
	// Get immature count from previous window
	var immprev int64
	if len(s.immatureCount) >= int(intervalSize) {
		immprev = int64(s.immatureCount[0])
		//fmt.Println(immprev, len(s.immatureCount), s.tip.height)
	}
	// apply it to previous live count
	p += immprev

	// Pool size change over last intervalSize blocks
	//
	// Option 1: fraction of previous:
	//poolDelta := float64(c) / float64(p)
	//
	// Option 2: fraction of max possible change
	// Pool size change is on [A,B] (i.e. [-720,1440] for mainnet)
	poolDelta := float64(c - p)
	// Compute fraction
	poolDelta = 1 + poolDelta/float64(B)/4.0
	// allow convergence
	if math.Abs(poolDelta-1) < 0.05 {
		poolDelta = 1
	}
	// no change -> 1, fall by 720 -> 0.75, increase by 1440 -> 1.25

	// Pool force (multiple of target pool size, signed)
	del := float64(c-t) / float64(t)
	// del /= float64(s.params.MaxFreshStakePerBlock)

	// Price velocity damper (always positive) - A large change in price from
	// the previous window to the current will have the effect of attenuating
	// the price change we are computing here. This is a very simple way of
	// slowing down the price, at the expense of making the price a little jumpy
	// and slower to adapt to big events.
	//
	// Magnitude of price change as a percent of previous price.
	absPriceDeltaLast := math.Abs(float64(curDiff-q) / float64(q))
	// Mapped onto (0,1] by an exponential decay
	m := math.Exp(-absPriceDeltaLast * 2) // m = 80% ~= exp((10% delta) *-2)
	// NOTE: make this stochastic by replacing the number 8 something like
	// (rand.NewSource(s.tip.ticketPrice).Int63() >> 59)

	// Scale directional (signed) pool force with the exponentially-mapped price
	// derivative. Interpret the scalar input parameter as a percent of this
	// computed price delta.
	pctChange := s1 / 100 * m * del
	n := float64(curDiff) * (1.0 + pctChange) * poolDelta

	// Enforce minimum and maximum prices
	pMax := int64(s.tip.totalSupply) / int64(s.params.TicketPoolSize)
	price := int64(n)
	if price < altMinDiff {
		price = altMinDiff
	} else if price > pMax {
		price = pMax
	}

	// Verbose info
	c -= int64(len(s.immatureTickets))
	fmt.Println(c, c-t+1280, m, poolDelta, curDiff, q, absPriceDeltaLast, pctChange, price)

	return price
}

func (s *simulator) poolSizeAndDiff(height int32) (int64, int64) {
	node := s.ancestorNode(s.tip, height, nil)
	if node != nil {
		return int64(node.poolSize), node.ticketPrice
	}
	return int64(s.tip.poolSize), s.tip.ticketPrice
}

// calcNextStakeDiffProposal1 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by raedah in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal1() int64 {
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

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// Attempt to get the pool size from the previous retarget interval.
	var prevPoolSize int64
	prevRetargetHeight := nextHeight - int32(intervalSize)
	node := s.ancestorNode(s.tip, prevRetargetHeight, nil)
	if node != nil {
		prevPoolSize = int64(node.poolSize)
	}

	// Return the existing ticket price for the first interval.
	if prevPoolSize == 0 {
		return curDiff
	}

	curPoolSize := int64(s.tip.poolSize)
	ratio := float64(curPoolSize) / float64(prevPoolSize)
	return int64(float64(curDiff) * ratio)
}

// calcNextStakeDiffProposal2 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by animedow in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal2() int64 {
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

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	//                ax
	// f(x) = - ---------------- + d
	//           (x - b)(x + c)
	//
	// x = amount of ticket deviation from the target pool size;
	// a = a modifier controlling the slope of the function;
	// b = the maximum boundary;
	// c = the minimum boundary;
	// d = the average ticket price in pool.
	x := int64(s.tip.poolSize) - (int64(s.params.TicketsPerBlock) *
		int64(s.params.TicketPoolSize))
	a := int64(100000)
	b := int64(2880)
	c := int64(2880)
	var d int64
	var totalSpent int64
	totalTickets := int64(len(s.immatureTickets) + s.liveTickets.Len())
	if totalTickets != 0 {
		for _, ticket := range s.immatureTickets {
			totalSpent += int64(ticket.price)
		}
		s.liveTickets.ForEach(func(k tickettreap.Key, v *tickettreap.Value) bool {
			totalSpent += v.PurchasePrice
			return true
		})
		d = totalSpent / totalTickets
	}
	price := int64(float64(d) - 100000000*(float64(a*x)/float64((x-b)*(x+c))))
	if price < s.params.MinimumStakeDiff {
		price = s.params.MinimumStakeDiff
	}
	return price
}

// calcNextStakeDiffProposal3 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by coblee in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal3() int64 {
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

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// f(x) = x*(locked/target_pool_size) + (1-x)*(locked/pool_size_actual)
	ticketsPerBlock := int64(s.params.TicketsPerBlock)
	targetPoolSize := ticketsPerBlock * int64(s.params.TicketPoolSize)
	lockedSupply := s.tip.stakedCoins
	x := int64(1)
	var price int64
	if s.tip.poolSize == 0 {
		price = int64(lockedSupply) / targetPoolSize
	} else {
		price = x*int64(lockedSupply)/targetPoolSize +
			(1-x)*(int64(lockedSupply)/int64(s.tip.poolSize))
	}
	if price < s.params.MinimumStakeDiff {
		price = s.params.MinimumStakeDiff
	}
	return price
}

// calcNextStakeDiffProposal4 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by jyap808 in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal4() int64 {
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

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// Get the number of tickets purchased in the previous interval.
	var ticketsPurchased int64
	prevRetargetHeight := s.tip.height - int32(intervalSize)
	s.ancestorNode(s.tip, prevRetargetHeight, func(node *blockNode) {
		ticketsPurchased += int64(len(node.ticketsAdded))
	})

	// Shorter versions of useful params for convenience.
	votesPerBlock := int64(s.params.TicketsPerBlock)
	votesPerInterval := votesPerBlock * int64(s.params.TicketPoolSize)
	maxTicketsPerBlock := int64(s.params.MaxFreshStakePerBlock)
	maxTicketsPerInterval := maxTicketsPerBlock * int64(s.params.TicketPoolSize)
	targetPoolSize := votesPerBlock * int64(s.params.TicketPoolSize)

	// Formulas provided by proposal.
	//
	// Bounds = TickPrice *  TickVotesCycle / MaxTickCycle
	// ScalingFactor = (TickBought - TickVotesCycle) / (MaxTickCycle - TickVotesCycle)
	//
	// If PoolTarget >= PoolTickets:
	//   NewTickPrice = TickPrice + (Bounds * Scaling Factor)
	// Else:
	//   NewTickPrice = TickPrice + (-Bounds * Scaling Factor)
	//
	var nextDiff int64
	bounds := float64(curDiff) * float64(votesPerInterval) /
		float64(maxTicketsPerInterval)
	scalingFactor := float64(ticketsPurchased-votesPerInterval) /
		float64(maxTicketsPerInterval-votesPerInterval)
	if targetPoolSize >= int64(s.tip.poolSize) {
		nextDiff = int64(float64(curDiff) + (bounds * scalingFactor))
	} else {
		nextDiff = int64(float64(curDiff) + (-bounds * scalingFactor))
	}

	if nextDiff < s.params.MinimumStakeDiff {
		nextDiff = s.params.MinimumStakeDiff
	}
	return nextDiff
}

var integral = 0.0
var previousError = 0.0

// calcNextStakeDiffProposal5 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by edsonbrusque in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal5() int64 {
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

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	ticketsPerBlock := int64(s.params.TicketsPerBlock)
	targetPoolSize := ticketsPerBlock * int64(s.params.TicketPoolSize)

	Kp := 0.0017
	Ki := 0.00005
	Kd := 0.0024
	e := float64(int64(s.tip.poolSize) - targetPoolSize)
	integral = integral + e
	derivative := (e - previousError)
	nextDiff := int64(dcrutil.AtomsPerCoin * (e*Kp + integral*Ki + derivative*Kd))
	previousError = e

	if nextDiff < s.params.MinimumStakeDiff {
		nextDiff = s.params.MinimumStakeDiff
	}
	return nextDiff
}
