package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dss "github.com/ipfs/go-datastore/sync"
	graphsync "github.com/ipfs/go-graphsync"
	gsimpl "github.com/ipfs/go-graphsync/impl"
	gsnet "github.com/ipfs/go-graphsync/network"
	storeutil "github.com/ipfs/go-graphsync/storeutil"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	chunker "github.com/ipfs/go-ipfs-chunker"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	files "github.com/ipfs/go-ipfs-files"
	ipldformat "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs/importer/balanced"
	ihelper "github.com/ipfs/go-unixfs/importer/helpers"
	ipld "github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	sel "github.com/myelnet/pop/selectors"
)

func CreateRandomBytes(ctx context.Context) ipld.LinkSystem {

	// create blockstore
	ds := dss.MutexWrap(datastore.NewMapDatastore())
	bs := blockstore.NewGCBlockstore(blockstore.NewBlockstore(ds), blockstore.NewGCLocker())
	lsys := storeutil.LinkSystemForBlockstore(bs)

	// random data
	data := make([]byte, 104857600)
	_, err := rand.New(rand.NewSource(time.Now().UnixNano())).Read(data)

	buf := bytes.NewReader(data)
	file := files.NewReaderFile(buf)

	dagService := merkledag.NewDAGService(blockservice.New(bs, offline.Exchange(bs)))

	// import to UnixFS
	bufferedDS := ipldformat.NewBufferedDAG(ctx, dagService)

	params := ihelper.DagBuilderParams{
		Maxlinks:   1024,
		RawLeaves:  true,
		CidBuilder: nil,
		Dagserv:    bufferedDS,
	}

	// split data into 1024000 bytes size chunks then DAGify it
	db, err := params.New(chunker.NewSizeSplitter(file, int64(1024000)))
	nd, err := balanced.Layout(db)
	err = bufferedDS.Commit()

	if err != nil {
		panic(err)
	}

	fmt.Printf("%s\n", nd.Cid().String())

	return lsys

}

func main() {

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	// if we haven't passed any argument then we're running a listening node
	listener := len(os.Args) == 1

	// default transport is Websockets
	opts := []libp2p.Option{
		libp2p.DefaultTransports,
	}

	// makes sure the listening node runs on a different port
	if listener {
		opts = append(opts, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/7878"))
	}

	// create a new libp2p host
	h, err := libp2p.New(opts...)
	if err != nil {
		panic(err)
	}

	// print the host peer ID / multi-address
	for _, m := range h.Addrs() {
		fmt.Printf("%s/p2p/%s\n", m, h.ID())
	}

	// graphsync network interface
	network := gsnet.NewFromLibp2pHost(h)

	// create random bytes and populates a blockstore with them
	lsys := CreateRandomBytes(ctx)

	exchange := gsimpl.New(ctx, network, lsys)

	// automatically validate incoming requests for content
	exchange.RegisterIncomingRequestHook(func(p peer.ID, request graphsync.RequestData, hookActions graphsync.IncomingRequestHookActions) {
		hookActions.ValidateRequest()
	})

	if !listener {

		// read in peer to dial
		ai, err := peer.AddrInfoFromString(os.Args[1])
		if err != nil {
			panic(err)
		}
		// read in cid to request
		cid1, err := cid.Decode(os.Args[2])
		if err != nil {
			panic(err)
		}

		// dial peer
		if err := h.Connect(ctx, *ai); err != nil {
			panic(err)
		}

		start := time.Now()
		// request peer for CID
		responses, _ := exchange.Request(ctx, ai.ID, cidlink.Link{cid1}, sel.All())
		// iterate until empty response
		for range responses {
		}
		took := time.Since(start)

		fmt.Printf("transfer took %s (%d bps)\n", took, int(float64(104857600)/took.Seconds()))
		return
	}

	select {}

}
