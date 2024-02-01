package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/highxshell/blockchainProject/core"
	"github.com/highxshell/blockchainProject/crypto"
	"github.com/highxshell/blockchainProject/network"
	"github.com/highxshell/blockchainProject/types"
	"github.com/highxshell/blockchainProject/util"
)

func main() {
	validatorPrivKey := crypto.GeneratePrivateKey()
	localNode := makeServer("LOCAL_NODE", &validatorPrivKey, ":3000", []string{":4000"}, ":9000")
	go localNode.Start()
	remoteNode := makeServer("REMOTE_NODE", nil, ":4000", []string{":5000"}, "")
	go remoteNode.Start()
	remoteNodeB := makeServer("REMOTE_NODE_B", nil, ":5000", nil, "")
	go remoteNodeB.Start()

	go func() {
		time.Sleep(11 * time.Second)
		lateNode := makeServer("LATE_NODE", nil, ":6000", []string{":4000"}, "")
		go lateNode.Start()
	}()

	time.Sleep(1 * time.Second)
	// if err := sendTransaction(validatorPrivKey); err != nil {
	// 	panic(err)
	// }
	//collectionOwnerPrivKey := crypto.GeneratePrivateKey()
	//collectionHash := createCollectionTx(collectionOwnerPrivKey)
	// txSendTicker := time.NewTicker(1 * time.Second)
	// go func() {
	// 	for i := 0; i < 20; i++ {
	// 		nftMinter(collectionOwnerPrivKey, collectionHash)

	// 		<-txSendTicker.C
	// 	}
	// }()
	select {}
}

func sendTransaction(privKey crypto.PrivateKey) error {
	toPrivKey := crypto.GeneratePrivateKey()
	tx := core.NewTransaction(nil)
	tx.To = toPrivKey.PublicKey()
	tx.Value = 666
	if err := tx.Sign(privKey); err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	if err := tx.Encode(core.NewGobTxEncoder(buf)); err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", "http://localhost:9000/tx", buf)
	if err != nil {
		panic(err)
	}
	client := http.Client{}
	_, err = client.Do(req)

	return err
}

func makeServer(id string, pk *crypto.PrivateKey, addr string, seedNodes []string, apiListenAddr string) *network.Server {
	options := network.ServerOptions{
		APIListenAddr: apiListenAddr,
		SeedNodes:     seedNodes,
		ListenAddress: addr,
		PrivateKey:    pk,
		ID:            id,
	}
	s, err := network.NewServer(options)
	if err != nil {
		log.Fatal(err)
	}

	return s
}

func createCollectionTx(privKey crypto.PrivateKey) types.Hash {
	tx := core.NewTransaction(nil)
	tx.TxInner = core.CollectionTx{
		Fee:      200,
		MetaData: []byte("chicken and egg collection!"),
	}
	tx.Sign(privKey)
	buf := &bytes.Buffer{}
	if err := tx.Encode(core.NewGobTxEncoder(buf)); err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", "http://localhost:9000/tx", buf)
	if err != nil {
		panic(err)
	}
	client := http.Client{}
	_, err = client.Do(req)
	if err != nil {
		panic(err)
	}

	return tx.Hash(core.TxHasher{})
}

func nftMinter(privKey crypto.PrivateKey, collection types.Hash) {
	metaData := map[string]any{
		"power":  8,
		"health": 100,
		"color":  "green",
		"rare":   "yes",
	}
	metaBuf := new(bytes.Buffer)
	if err := json.NewEncoder(metaBuf).Encode(metaData); err != nil {
		panic(err)
	}
	tx := core.NewTransaction(nil)
	tx.TxInner = core.MintTx{
		Fee:             200,
		NFT:             util.RandomHash(),
		Metadata:        metaBuf.Bytes(),
		Collection:      collection,
		CollectionOwner: privKey.PublicKey(),
	}
	tx.Sign(privKey)
	buf := &bytes.Buffer{}
	if err := tx.Encode(core.NewGobTxEncoder(buf)); err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", "http://localhost:9000/tx", buf)
	if err != nil {
		panic(err)
	}
	client := http.Client{}
	_, err = client.Do(req)
	if err != nil {
		panic(err)
	}
}

func contract() []byte {
	data := []byte{0x02, 0x0a, 0x03, 0x0a, 0x0b, 0x4f, 0x0c, 0x4f, 0x0c, 0x46, 0x0c, 0x03, 0x0a, 0x0d, 0x0f}
	pushFoo := []byte{0x4f, 0x0c, 0x4f, 0x0c, 0x46, 0x0c, 0x03, 0x0a, 0x0d, 0xae}
	data = append(data, pushFoo...)

	return data
}
