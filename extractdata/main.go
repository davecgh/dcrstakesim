// Copyright (c) 2017 Dave Collins
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

func main() {
	// Connect to local dcrd RPC server using websockets.
	dcrdHomeDir := dcrutil.AppDataDir("dcrd", false)
	certs, err := ioutil.ReadFile(filepath.Join(dcrdHomeDir, "rpc.cert"))
	if err != nil {
		log.Fatal(err)
	}
	connCfg := &dcrrpcclient.ConnConfig{
		Host:         "localhost:9109",
		Endpoint:     "ws",
		User:         "yourrpcuser",
		Pass:         "yourrpcpass",
		Certificates: certs,
	}
	client, err := dcrrpcclient.New(connCfg, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Get the current block count.
	blockCount, err := client.GetBlockCount()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Height,Header,Ticket Hashes")
	for i := int64(0); i <= blockCount; i++ {
		hash, err := client.GetBlockHash(i)
		if err != nil {
			log.Fatal(err)
		}
		block, err := client.GetBlock(hash)
		if err != nil {
			log.Fatal(err)
		}

		headerBytes, err := block.MsgBlock().Header.Bytes()
		if err != nil {
			log.Fatal(err)
		}

		var ticketHashes []string
		for _, stx := range block.STransactions() {
			if ok, _ := stake.IsSStx(stx.MsgTx()); ok {
				ticketHash := stx.MsgTx().TxHash().String()
				ticketHashes = append(ticketHashes, ticketHash)
			}
		}

		fmt.Printf("%d,%x,%v\n", i, headerBytes,
			strings.Join(ticketHashes, ":"))
	}

	client.Shutdown()
	client.WaitForShutdown()
}
