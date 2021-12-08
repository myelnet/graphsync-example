# graphsync-example

Here we outline a simple toy example (`main.go`) where two local peers transfer Interplanetary Linked Data (IPLD) graphs using Graphsync and Libp2p. 

First lets review some core concepts.

### Libp2p

Libp2p is a modular interface that manages identity, transports, security, peer routing, content discovery, and messaging for peer to peer (p2p) network applications (for a primer on p2p networking see [here](https://pediaa.com/difference-between-peer-to-peer-and-client-server-network/)) ! That's quite the list of abilities so lets begin by unpacking each one in turn. 

- **Transport**:  the transport layer is responsible for the actual transmission and receipt of data from one peer to another.
- **Identity**: libp2p uses [public key cryptography](https://en.wikipedia.org/wiki/Public-key_cryptography) as the basis of peer identity. Each peer has a globally unique `PeerId`, which allows anyone to retrieve the public key for the identified peer, and enables secure communication between peers.
- **Security**: libp2p supports “upgrading” a connection provided by a [transport](https://docs.libp2p.io/introduction/what-is-libp2p/#transport) into a securely encrypted channel. The process is flexible, and can  support multiple methods of encrypting communication. libp2p currently  supports [TLS 1.3](https://www.ietf.org/blog/tls13/) and [Noise](https://noiseprotocol.org/). 
- **Routing**: libp2p implements multiple methods for locating a peer within a network, using solely their `PeerId`. The current stable implementation uses the  [Kademlia](https://en.wikipedia.org/wiki/Kademlia) routing algorithm.
- **Content Discovery**:  libp2p implements multiple methods for locating a piece of content (often content-addressed data, which is content uniquely identified by a "content ID" (CID) which itself is based on the content’s [cryptographic hash](https://docs.ipfs.io/concepts/hashing/), see [here](https://docs.ipfs.io/concepts/content-addressing/) for a primer on content addressing) within a network. Also currently uses the  [Kademlia](https://en.wikipedia.org/wiki/Kademlia) routing algorithm. 
- **Messaging**: libp2p defines a [pubsub interface](https://github.com/libp2p/specs/tree/master/pubsub) for sending messages to all peers subscribed to a given “topic”. 

### IPLD

Interplanetary Linked Data (IPLD) is a powerful specification for decentralized and content addressed data. 

Data, specified using IPLD, is represented as a graph, specifically a [Directed Acyclic Graph](https://en.wikipedia.org/wiki/Directed_acyclic_graph) (DAG). This allows for powerful capabilities, including the ability to hierarchically link together documents within a single DAG. For more details on how this is done see [here](https://proto.school/merkle-dags/05). 

**As a brief overview; here are some core components of an IPLD DAG:** 

- Block: A block is a chunk of an IPLD DAG, encoded in a format. Blocks have [CID](https://github.com/ipld/specs/blob/master/block-layer/CID.md)s.
- Node: A node is a *point* in an IPLD DAG (map, list, number, etc.). Many nodes can exist encoded inside one Block.
- Link: A link is a kind of IPLD Node that points to another IPLD Node. Links are what make IPLD data a DAG rather than only a tree. Links themselves are content-addressable -- see [CID](https://github.com/ipld/specs/blob/master/block-layer/CID.md).
- Path Segment: A path segment is a piece of information that describes a move from one Node to a directly connected child Node.  (In other words, a Path Segment is either a map key or a list index.)
- Path: A path is composed of Path Segments, thereby describing a traversal from one Node to another Node somewhere deeper in the DAG.


Another important concept is that of IPLD selectors. **IPLD Selectors** are expressions that identify ("select") a subset of nodes in an IPLD dag. Visually ([source](https://github.com/ipld/specs/blob/master/selectors/selectors.md)): 

![https://github.com/ipld/specs/blob/master/selectors/selectors.jpg?raw=true](https://github.com/ipld/specs/blob/master/selectors/selectors.jpg?raw=true)



### Graphsync ? 

> [GraphSync](https://github.com/ipld/specs/blob/master/block-layer/graphsync/graphsync.md) is a protocol for synchronizing IPLD graphs among peers. It allows a  host to make a single request to a remote peer for all of the results of traversing an [IPLD selector](https://ipld.io/specs/selectors/) on the remote peer's local IPLD graph.

The exact way this is done is rather complex ([source](https://github.com/ipfs/go-graphsync/blob/main/docs/architecture.md)): 

![https://github.com/ipfs/go-graphsync/raw/main/docs/top-level-sequence.png](https://github.com/ipfs/go-graphsync/raw/main/docs/top-level-sequence.png)

*More simply* go-graphsync can be roughly divided into four major components.

1. The top Level interface implemented in the root module is  called by a GraphSync client to initiate a request or as incoming  GraphSync related messages are received from the network.
2. The Graphsync requestor implementation makes requests to the network and handles incoming GraphSync responses.
3. The Graphsync responder implementation handles incoming GraphSync requests from the network and generates responses.
4. The message sending layer manages to send messages to  peers. It is shared by both the requestor implementation and the  responder implementation

go-graphsync also depends on the following external dependencies:

1. A network implementation, which provides basic functions for sending and receiving messages on a network.
2. A local blockstore implementation, expressed by an IPLD `LinkSystem` (to store the DAGs !)

Note that Graphsync requests can include a selector for a specific sub-graph within an IPLD DAG -- allowing for clients to specifically retrieve the pieces of data that are relevant to them !

These concepts will be critical as we work our way through the example.

### Instantiating our Graphsync network interfaces 

We're going to create a script, which depending on the passed arguments, creates a local node that acts as a listener, awaiting Graphsync requests, or creates a local requesting node, which sends out requests for content. This can be done simply: 


```go
func main() {
// if we haven't passed any argument then we're running a listening node
	listener := len(os.Args) == 1
// ... 
```

As we mentioned above, Libp2p is the foundational interface that we're going to use for networking. We use the Libp2p default transports (TCP websockets) and initialize a new node: 

```go
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
```

As mentioned above, Libp2p assigns a unique `peerID` to each peer on the network.

```go
  // print the host peer ID / multi-address
	for _, m := range h.Addrs() {
		fmt.Printf("%s/p2p/%s\n", m, h.ID())
	}
```

Given that Libp2p serves as the backbone of our p2p networking, we can use this to instantiate our Graphsync network interface, which if you recall provides basic functions for sending and receiving Graphsync requests / responses on a network. 


```go
  // graphsync network interface
	network := gsnet.NewFromLibp2pHost(h)
```

### Instantiating our Graphsync requester/responder

The Graphsync requester and responder interfaces are collectively wrapped into the Graphsync "exchange" in the `golang` implementation of Graphsync. This exchange sits on top of, and depends on, a blockstore expressed by an IPLD `LinkSystem` (to store the DAGs !). 

So the first thing we need to do is to instantiate this blockstore, populate it with data, and return a `LinkSystem` object that can be consumed by the exchange. We do this using the `CreateRandomBytes` function, called as such: 

``` go
	// create random bytes and populates a blockstore with them
	lsys := CreateRandomBytes(ctx, dataSize) 
```

This function takes in a `golang` context object and a data size integer, representing the size of the random data (in bytes) that we want to generate and store. 

#### <u>CreateRandomBytes</u>:

Within this function the first thing we do is instantiate the blockstore and an associated IPLD `LinkSystem` : 

```go
func CreateRandomBytes(ctx context.Context, dataSize int) ipld.LinkSystem {
  // create blockstore
	ds := dss.MutexWrap(datastore.NewMapDatastore())
	bs := blockstore.NewGCBlockstore(blockstore.NewBlockstore(ds), blockstore.NewGCLocker())
	lsys := storeutil.LinkSystemForBlockstore(bs)
  dagService := merkledag.NewDAGService(blockservice.New(bs, offline.Exchange(bs)))
  // ...
```

There's a lot to unpack there so let's run through it line by line. 

- `MutexWrap`: creates a simple API for Querying, Putting, Reading etc... elements from key-value datastores. We now have a **datastore**. 
- `NewGCBlockstore`: our data is structured as blocks, each with an associated CID (see the IPLD definition of a block above), and we need a simple interface for putting and getting these special objects from a datastore. This is what `NewGCBlockstore` provides. We now have a **blockstore**. 
-  `LinkSystemForBlockstore`: If you recall, a crucial component of IPLD DAGs  are *links*, which point from one DAG node to another (they make a DAG a DAG and not just a tree or some random soup of blocks). We need a way to manage these links on top of our blockstore, which is what `LinkSystemForBlockstore` provides. We can now **store IPLD DAGs** ! 
- `NewDAGService`: Is just an interface for putting and putting DAG nodes and Links. 

So we now have a simple interface for storing our DAGs ! 

Let's create some random data and commit it to our blockstore. 

```go
  // random data
  data := make([]byte, dataSize)
	_, err := rand.New(rand.NewSource(time.Now().UnixNano())).Read(data)

	buf := bytes.NewReader(data)
	file := files.NewReaderFile(buf)

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
```

Here we've created an empty DAG buffer (in [UnixFS](https://github.com/ipfs/go-unixfs/) format, `NewBufferedDAG`), we've split our data into chunks  (`NewSizeSplitter`, its **best practice to split large files into multiple chunks**, each one a block in the DAG), DAGified our chunks (`balanced.Layout`) then commited our DAG to the blockstore (`bufferedDS.Commit()`). 

`balanced.Layout` creates what is called a "balanced" DAG from the chunks of data, which are generalistic DAGs in which  all leaves (nodes representing chunks of data) are at the same distance from the root node in the DAG. Eg (for more details [source](https://github.com/ipfs/go-unixfs/blob/master/importer/balanced/builder.go)): 

```
//                                                 +-------------+
//                                                 |   Root 4    |
//                                                 +-------------+
//                                                       |
//                            +--------------------------+----------------------------+
//                            |                                                       |
//                      +-------------+                                         +-------------+
//                      |   Node 2    |                                         |   Node 5    |
//                      +-------------+                                         +-------------+
//                            |                                                       |
//              +-------------+-------------+                           +-------------+
//              |                           |                           |
//       +-------------+             +-------------+             +-------------+
//       |   Node 1    |             |   Node 3    |             |   Node 6    |
//       +-------------+             +-------------+             +-------------+
//              |                           |                           |
//       +------+------+             +------+------+             +------+
//       |             |             |             |             |
//  +=========+   +=========+   +=========+   +=========+   +=========+
//  | Chunk 1 |   | Chunk 2 |   | Chunk 3 |   | Chunk 4 |   | Chunk 5 |
//  +=========+   +=========+   +=========+   +=========+   +=========+
```

We can print the Root CID, created by `balanced`: 

```go
fmt.Printf("%s\n", nd.Cid().String())
```

Finally the function returns the `LinkSystem` which we need to initialize our Graphsync exchange. 

```go
return lsys
}
```
Returning to our `main()` function we can now initialize our Graphsync exchange, using the `LinkSystem` populated with data. We also specify that our exchange should accept incoming requests by default.

```go
// create random bytes and populates a blockstore with them
	lsys := CreateRandomBytes(ctx, dataSize)

	exchange := gsimpl.New(ctx, network, lsys)

	// automatically validate incoming requests for content
	exchange.RegisterIncomingRequestHook(func(p peer.ID, request graphsync.RequestData, hookActions graphsync.IncomingRequestHookActions) {
		hookActions.ValidateRequest()
	})
```

That's all the code we need for the listener node ! 

### Making Graphsync Requests

If we are not in listening mode, we want to be able to make requests to other nodes listening for inbound requests. We read in the peer we want to dial and the CID we are requesting as arguments to `main.go`. 


```go
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
```

We then make sure the peer is dialable

```go
    // dial peer
		if err := h.Connect(ctx, *ai); err != nil {
			panic(err)
		} 
```

We can now ask the peer for that CID and measure the time the response takes.  

```go
    start := time.Now()
		// request peer for CID
		responses, _ := exchange.Request(ctx, ai.ID, cidlink.Link{cid1}, sel.All())
		// iterate until empty response
		for range responses {
		}
		took := time.Since(start)
    fmt.Printf("transfer took %s (%d bps)\n", took, int(float64(dataSize)/took.Seconds()))
		return
	}
```

`sel.All()` is an IPLD selector (see above), which specifies that we want to retrieve the entire DAG ! 

### Running listeners and requesters

The code in its entirety can be found in `main.go`

- To run a listening node run `go run main.go` . This will print our the listener's peer ID and the CID of the random DAG we've created. 
- Open a new terminal window and copy paste that peer ID and CID as arguments to `main.go` for instance: 

```bash
go run main.go /ip4/127.0.0.1/tcp/7878/p2p/QmTSHRrQdSf4cjgHkNXncFpQxXUwwbaUtuwDjNq2CpSi6d QmaQgoifNnV3hZaZMGEh8J178WBeMSTRKWk67qXwLB2jj1
```

This will print the time it took to retrieve the DAG over Graphsync ! 



