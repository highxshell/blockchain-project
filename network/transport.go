package network

import "net"

type NetAddress string

type Transport interface {
	Consume() <-chan RPC
	Connect(Transport) error
	SendMessage(net.Addr, []byte) error
	Broadcast([]byte) error
	Address() net.Addr
}
