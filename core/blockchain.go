package core

import (
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/highxshell/blockchainProject/crypto"
	"github.com/highxshell/blockchainProject/types"
)

type Blockchain struct {
	logger log.Logger
	store  Storage
	// TODO: double check this!
	mu         sync.RWMutex
	headers    []*Header
	blocks     []*Block
	txStore    map[types.Hash]*Transaction
	blockStore map[types.Hash]*Block

	accountState *AccountState

	stateMu         sync.RWMutex
	collectionState map[types.Hash]*CollectionTx
	mintState       map[types.Hash]*MintTx
	validator       Validator
	// TODO: make this an interface.
	contractState *State
}

func NewBlockchain(l log.Logger, genesis *Block) (*Blockchain, error) {
	// We should create all states inside the scope of the newBlockchain.

	// TODO: read this from disk later on
	accountState := NewAccountState()

	coinbase := crypto.PublicKey{}
	accountState.CreateAccount(coinbase.Address())

	bc := &Blockchain{
		contractState:   NewState(),
		headers:         []*Header{},
		store:           NewMemoryStore(),
		logger:          l,
		accountState:    accountState,
		blockStore:      make(map[types.Hash]*Block),
		txStore:         make(map[types.Hash]*Transaction),
		collectionState: make(map[types.Hash]*CollectionTx),
		mintState:       make(map[types.Hash]*MintTx),
	}
	bc.validator = NewBlockValidator(bc)
	err := bc.addBlockWithoutValidation(genesis)

	return bc, err
}

func (bc *Blockchain) SetValidator(v Validator) {
	bc.validator = v
}

func (bc *Blockchain) AddBlock(b *Block) error {
	if err := bc.validator.ValidateBlock(b); err != nil {
		return err
	}

	return bc.addBlockWithoutValidation(b)
}

func (bc *Blockchain) handleNativeTransfer(tx *Transaction) error {
	bc.logger.Log(
		"msg", "handle native token transfer",
		"from", tx.From,
		"to", tx.To,
		"value", tx.Value)

	return bc.accountState.Transfer(tx.From.Address(), tx.To.Address(), tx.Value)
}

func (bc *Blockchain) handleNativeNFT(tx *Transaction) error {
	hash := tx.Hash(TxHasher{})
	switch t := tx.TxInner.(type) {
	case CollectionTx:
		bc.collectionState[hash] = &t
		bc.logger.Log("msg", "created new NFT collection", "hash", hash)
	case MintTx:
		_, ok := bc.collectionState[t.Collection]
		if !ok {
			return fmt.Errorf("collection (%s) does not exist on the blockchain", t.Collection)
		}
		bc.mintState[hash] = &t

		bc.logger.Log("msg", "created new NFT mint", "NFT", t.NFT, "collection", t.Collection)
	default:
		return fmt.Errorf("unsupported tx type %v", t)
	}

	return nil
}

func (bc *Blockchain) GetBlockByHash(hash types.Hash) (*Block, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	block, ok := bc.blockStore[hash]
	if !ok {
		return nil, fmt.Errorf("block with hash (%s) not found", hash)
	}

	return block, nil
}

func (bc *Blockchain) GetBlock(height uint32) (*Block, error) {
	if height > bc.Height() {
		return nil, fmt.Errorf("given height (%d) too high", height)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()

	return bc.blocks[height], nil
}

func (bc *Blockchain) GetHeader(height uint32) (*Header, error) {
	if height > bc.Height() {
		return nil, fmt.Errorf("given height (%d) too high", height)
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()

	return bc.headers[height], nil
}

func (bc *Blockchain) GetTxByHash(hash types.Hash) (*Transaction, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	tx, ok := bc.txStore[hash]
	if !ok {
		return nil, fmt.Errorf("could not find tx with hash (%s)", hash)
	}

	return tx, nil
}

func (bc *Blockchain) HasBlock(height uint32) bool {
	return height <= bc.Height()
}

func (bc *Blockchain) Height() uint32 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return uint32(len(bc.headers) - 1)
}

func (bc *Blockchain) handleTransaction(tx *Transaction) error {
	// if we have data inside execute that data on the VM.
	if len(tx.Data) > 0 {
		bc.logger.Log("msg", "executing code", "len", len(tx.Data), "hash", tx.Hash(&TxHasher{}))
		vm := NewVM(tx.Data, bc.contractState)
		if err := vm.Run(); err != nil {
			return err
		}
	}
	// If the txInner of the transaction is not nil we need to handle the native
	// NFT implementation.
	if tx.TxInner != nil {
		if err := bc.handleNativeNFT(tx); err != nil {
			return err
		}
	}
	// Handle the native transaction here.
	if tx.Value > 0 {
		if err := bc.handleNativeTransfer(tx); err != nil {
			return err
		}
	}

	return nil
}

func (bc *Blockchain) addBlockWithoutValidation(b *Block) error {
	bc.stateMu.Lock()
	for i := 0; i < len(b.Transactions); i++ {
		if err := bc.handleTransaction(b.Transactions[i]); err != nil {
			bc.logger.Log("error", err.Error())
			b.Transactions[i] = b.Transactions[len(b.Transactions)-1]
			b.Transactions = b.Transactions[:len(b.Transactions)-1]
			continue
		}
	}
	bc.stateMu.Unlock()
	fmt.Println("==========ACCOUNT STATE=============")
	fmt.Printf("%+v\n", bc.accountState.accounts)
	fmt.Println("==========ACCOUNT STATE=============")
	bc.mu.Lock()
	bc.headers = append(bc.headers, b.Header)
	bc.blocks = append(bc.blocks, b)
	bc.blockStore[b.Hash(BlockHasher{})] = b
	for _, tx := range b.Transactions {
		bc.txStore[tx.Hash(TxHasher{})] = tx
	}
	bc.mu.Unlock()
	bc.logger.Log(
		"msg", "new block",
		"hash", b.Hash(BlockHasher{}),
		"height", b.Height,
		"transactions", len(b.Transactions),
	)

	return bc.store.Put(b)
}
