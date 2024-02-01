package network

import (
	"bytes"
	"fmt"
	"net"
	"sync"
)

type LocalTransport struct {
	address     net.Addr
	consumeChan chan RPC
	lock        sync.RWMutex
	peers       map[net.Addr]*LocalTransport
}

func NewLocalTransport(address net.Addr) *LocalTransport {
	return &LocalTransport{
		address:     address,
		consumeChan: make(chan RPC, 1024),
		peers:       make(map[net.Addr]*LocalTransport),
	}
}

func (t *LocalTransport) Consume() <-chan RPC {
	return t.consumeChan
}

func (t *LocalTransport) Connect(tr Transport) error {
	trans := tr.(*LocalTransport)
	t.lock.Lock()
	defer t.lock.Unlock()

	t.peers[tr.Address()] = trans

	return nil
}

func (t *LocalTransport) SendMessage(to net.Addr, payload []byte) error {
	t.lock.RLock()
	defer t.lock.RUnlock()

	if t.address == to {
		return nil
	}

	peer, ok := t.peers[to]
	if !ok {
		return fmt.Errorf("%s: could not send message to unknown peer: %s", t.address, to)
	}
	peer.consumeChan <- RPC{
		From:    t.address,
		Payload: bytes.NewReader(payload),
	}

	return nil
}

func (t *LocalTransport) Broadcast(payload []byte) error {
	for _, peer := range t.peers {
		if err := t.SendMessage(peer.Address(), payload); err != nil {
			return err
		}
	}

	return nil
}

func (t *LocalTransport) Address() net.Addr {
	return t.address
}
