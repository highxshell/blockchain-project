package network

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/highxshell/blockchainProject/api"
	"github.com/highxshell/blockchainProject/crypto"
	"github.com/highxshell/blockchainProject/types"

	"github.com/go-kit/log"
	"github.com/highxshell/blockchainProject/core"
)

var defaultBlockTime = 5 * time.Second

type ServerOptions struct {
	APIListenAddr string
	SeedNodes     []string
	ListenAddress string
	TCPTransport  *TCPTransport
	ID            string
	Logger        log.Logger
	RPCDecodeFunc RPCDecodeFunc
	RPCProcessor  RPCProcessor
	BlockTime     time.Duration
	PrivateKey    *crypto.PrivateKey
}

type Server struct {
	TCPTransport *TCPTransport
	peerChan     chan *TCPPeer

	mu      sync.RWMutex
	peerMap map[net.Addr]*TCPPeer

	ServerOptions
	memPool     *TxPool
	chain       *core.Blockchain
	isValidator bool
	rpcChan     chan RPC
	quitChan    chan struct{}
	txChan      chan *core.Transaction
}

func NewServer(options ServerOptions) (*Server, error) {
	if options.BlockTime == time.Duration(0) {
		options.BlockTime = defaultBlockTime
	}
	if options.RPCDecodeFunc == nil {
		options.RPCDecodeFunc = DefaultRPCDecodeFunc
	}
	if options.Logger == nil {
		options.Logger = log.NewLogfmtLogger(os.Stderr)
		options.Logger = log.With(options.Logger, "addr", options.ID)
	}

	chain, err := core.NewBlockchain(options.Logger, genesisBlock())
	if err != nil {
		return nil, err
	}

	// CHannel being used to communicate between the JSOn RPC server
	// and the node that will process this message
	txChan := make(chan *core.Transaction)

	// Only boot up the API server if the config has a valid port number.
	if len(options.APIListenAddr) > 0 {
		apiServerCfg := api.ServerConfig{
			Logger:     options.Logger,
			ListenAddr: options.APIListenAddr,
		}
		apiServer := api.NewServer(apiServerCfg, chain, txChan)
		go apiServer.Start()
		options.Logger.Log("msg", "JSON API server running", "port", options.APIListenAddr)
	}
	peerChan := make(chan *TCPPeer)
	tr := NewTCPTransport(options.ListenAddress, peerChan)
	s := &Server{
		TCPTransport:  tr,
		peerChan:      peerChan,
		peerMap:       make(map[net.Addr]*TCPPeer),
		ServerOptions: options,
		chain:         chain,
		memPool:       NewTxPool(1000),
		isValidator:   options.PrivateKey != nil,
		rpcChan:       make(chan RPC),
		quitChan:      make(chan struct{}, 1),
		txChan:        txChan,
	}
	s.TCPTransport.peerChan = peerChan
	// if we dont got any processor from the server options, we going to use
	// the server as default
	if s.RPCProcessor == nil {
		s.RPCProcessor = s
	}
	if s.isValidator {
		go s.ValidatorLoop()
	}

	return s, nil
}

func (s *Server) bootstrapNetwork() {
	for _, addr := range s.SeedNodes {
		fmt.Println("trying to ", addr)
		go func(addr string) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				fmt.Printf("could not connect to %+v\n", conn)
				return
			}
			s.peerChan <- &TCPPeer{
				conn: conn,
			}
		}(addr)
	}
}

func (s *Server) Start() {
	s.TCPTransport.Start()
	time.Sleep(1 * time.Second)
	s.bootstrapNetwork()
	s.Logger.Log("msg", "accepting TCP connection on", "addr", s.ListenAddress, "id", s.ID)
free:
	for {
		select {
		case peer := <-s.peerChan:
			s.peerMap[peer.conn.RemoteAddr()] = peer
			go peer.readLoop(s.rpcChan)
			if err := s.sendGetStatusMessage(peer); err != nil {
				s.Logger.Log("err", err)
				continue
			}
			s.Logger.Log("msg", "peer added to the server", "outgoing", peer.Outgoing, "addr", peer.conn.RemoteAddr())
		case tx := <-s.txChan:
			if err := s.processTransaction(tx); err != nil {
				s.Logger.Log("process TX error", err)
			}
		case rpc := <-s.rpcChan:
			msg, err := s.RPCDecodeFunc(rpc)
			if err != nil {
				s.Logger.Log("RPC error", err)
				continue
			}
			if err := s.RPCProcessor.ProcessMessage(msg); err != nil {
				if err != core.ErrBlockKnown {
					s.Logger.Log("error", err)
				}
			}

		case <-s.quitChan:
			break free
		}
	}

	s.Logger.Log("msg", "Server is shutting down")
}

func (s *Server) ValidatorLoop() {
	ticker := time.NewTicker(s.BlockTime)
	s.Logger.Log("msg", "Starting validator loop", "blockTime", s.BlockTime)
	for {
		fmt.Println("creating new block")
		if err := s.createNewBlock(); err != nil {
			s.Logger.Log("create block erorr", err)
		}
		<-ticker.C
	}
}

func (s *Server) ProcessMessage(msg *DecodedMessage) error {
	switch t := msg.Data.(type) {
	case *core.Transaction:
		return s.processTransaction(t)
	case *core.Block:
		return s.processBlock(t)
	case *GetStatusMessage:
		return s.processGetStatusMessage(msg.From, t)
	case *StatusMessage:
		return s.processStatusMessage(msg.From, t)
	case *GetBlocksMessage:
		return s.processGetBlocksMessage(msg.From, t)
	case *BlocksMessage:
		return s.processBlocksMessage(msg.From, t)
	}

	return nil
}

func (s *Server) processBlocksMessage(from net.Addr, data *BlocksMessage) error {
	s.Logger.Log("msg", "received BLOCKS!!!! message", "from", from)

	for _, block := range data.Blocks {
		if err := s.chain.AddBlock(block); err != nil {
			s.Logger.Log("erorr", err.Error())
			return err
		}
	}

	return nil
}

// AAA
func (s *Server) processGetBlocksMessage(from net.Addr, data *GetBlocksMessage) error {
	s.Logger.Log("msg", "received getBlocks message", "from", from)
	var (
		blocks    = []*core.Block{}
		ourHeight = s.chain.Height()
	)
	if data.To == 0 {
		for i := data.From; i <= ourHeight; i++ {
			block, err := s.chain.GetBlock(uint32(i))
			if err != nil {
				return err
			}
			blocks = append(blocks, block)
		}
	}

	blocksMsg := &BlocksMessage{
		Blocks: blocks,
	}
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(blocksMsg); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg := NewMessage(MessageTypeBlocks, buf.Bytes())
	peer, ok := s.peerMap[from]
	if !ok {
		return fmt.Errorf("peer %s not known", peer.conn.RemoteAddr())
	}

	return peer.Send(msg.Bytes())
}

func (s *Server) sendGetStatusMessage(peer *TCPPeer) error {
	var (
		getStatusMsg = new(GetStatusMessage)
		buf          = new(bytes.Buffer)
	)
	if err := gob.NewEncoder(buf).Encode(getStatusMsg); err != nil {
		return err
	}
	msg := NewMessage(MessageTypeGetStatus, buf.Bytes())

	return peer.Send(msg.Bytes())
}

func (s *Server) broadcast(payload []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for netAddr, peer := range s.peerMap {
		if err := peer.Send(payload); err != nil {
			fmt.Printf("peer send error => addr %s [err: %s]\n", netAddr, err)
		}
	}

	return nil
}

func (s *Server) processStatusMessage(from net.Addr, data *StatusMessage) error {
	s.Logger.Log("msg", "received STATUS message", "from", from)
	if data.CurrentHeight <= s.chain.Height() {
		s.Logger.Log("msg", "cannot sync blockHeight to low", "ourHeight", s.chain.Height(), "their height", data.CurrentHeight, "address", from)
		return nil
	}
	go s.requestBlocksLoop(from)

	return nil
}

func (s *Server) processGetStatusMessage(from net.Addr, data *GetStatusMessage) error {
	s.Logger.Log("msg", "received getStatus message", "from", from)
	statusMessage := &StatusMessage{
		CurrentHeight: s.chain.Height(),
		ID:            s.ID,
	}
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(statusMessage); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg := NewMessage(MessageTypeStatus, buf.Bytes())
	peer, ok := s.peerMap[from]
	if !ok {
		return fmt.Errorf("peer %s not known", peer.conn.RemoteAddr())
	}

	return peer.Send(msg.Bytes())
}

func (s *Server) processBlock(b *core.Block) error {
	if err := s.chain.AddBlock(b); err != nil {
		s.Logger.Log("erorr", err.Error())
		return err
	}
	go s.broadcastBlock(b)

	return nil
}

func (s *Server) processTransaction(tx *core.Transaction) error {
	hash := tx.Hash(core.TxHasher{})
	if s.memPool.Contains(hash) {
		return nil
	}
	if err := tx.Verify(); err != nil {
		return err
	}

	// s.Logger.Log(
	// 	"msg", "adding new tx to mempool",
	// 	"hash", hash,
	// 	"mempoolPending", s.memPool.PendingCount(),
	// )
	go s.broadcastTx(tx)
	s.memPool.Add(tx)

	return nil
}

// TODO: find a way we dont keep syncing when we are at the highest
// block height in the network
func (s *Server) requestBlocksLoop(from net.Addr) error {
	ticker := time.NewTicker(3 * time.Second)
	for {
		ourHeight := s.chain.Height()
		s.Logger.Log("msg", "requesting new blocks", "requesting height", ourHeight+1)
		// In this case we are 100% sure that the node has blocks higher than us.
		getBlocksMessage := &GetBlocksMessage{
			From: ourHeight + 1,
			To:   0,
		}
		buf := new(bytes.Buffer)
		if err := gob.NewEncoder(buf).Encode(getBlocksMessage); err != nil {
			return err
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		msg := NewMessage(MessageTypeGetBlocks, buf.Bytes())
		peer, ok := s.peerMap[from]
		if !ok {
			return fmt.Errorf("peer %s not known", peer.conn.RemoteAddr())
		}
		if err := peer.Send(msg.Bytes()); err != nil {
			s.Logger.Log("error", "failed to send to peer", "err", err, "addr", from)
		}

		<-ticker.C
	}
}

func (s *Server) broadcastBlock(b *core.Block) error {
	buf := &bytes.Buffer{}
	if err := b.Encode(core.NewGobBlockEncoder(buf)); err != nil {
		return err
	}
	msg := NewMessage(MessageTypeBlock, buf.Bytes())

	return s.broadcast(msg.Bytes())
}

func (s *Server) broadcastTx(tx *core.Transaction) error {
	buf := &bytes.Buffer{}
	if err := tx.Encode(core.NewGobTxEncoder(buf)); err != nil {
		return err
	}

	msg := NewMessage(MessageTypeTx, buf.Bytes())

	return s.broadcast(msg.Bytes())
}

func (s *Server) createNewBlock() error {
	currentHeader, err := s.chain.GetHeader(s.chain.Height())
	if err != nil {
		return err
	}

	// For now we are going to use all transactions that are in the mempool
	// Later on when we know the internal structure of our transaction
	// we will implement some kind of complexity function to determine how
	// many transactions can be included in a block.
	txx := s.memPool.Pending()

	block, err := core.NewBlockFromPrevHeader(currentHeader, txx)
	if err != nil {
		return err
	}

	if err := block.Sign(*s.PrivateKey); err != nil {
		return err
	}

	if err := s.chain.AddBlock(block); err != nil {
		return err
	}

	s.memPool.ClearPending()

	go s.broadcastBlock(block)

	return nil
}

func genesisBlock() *core.Block {
	header := &core.Header{
		Version:   1,
		DataHash:  types.Hash{},
		Height:    0,
		Timestamp: 000000,
	}
	b, _ := core.NewBlock(header, nil)

	coinbase := crypto.PublicKey{}
	tx := core.NewTransaction(nil)
	tx.From = coinbase
	tx.To = coinbase
	tx.Value = 10_000_000

	b.Transactions = append(b.Transactions, tx)

	privKey := crypto.GeneratePrivateKey()
	if err := b.Sign(privKey); err != nil {
		panic(err)
	}

	return b
}
