package network

import (
	"bytes"
	"fmt"
	"io"
	"net"
)

type TCPPeer struct {
	conn     net.Conn
	Outgoing bool
}

func (p *TCPPeer) Send(payload []byte) error {
	_, err := p.conn.Write(payload)
	return err
}

func (p *TCPPeer) readLoop(rpcChan chan RPC) {
	buf := make([]byte, 4096)
	for {
		n, err := p.conn.Read(buf)
		if err == io.EOF {
			continue
		}
		if err != nil {
			fmt.Printf("read error: %s", err)
			continue
		}
		msg := buf[:n]
		rpcChan <- RPC{
			From:    p.conn.RemoteAddr(),
			Payload: bytes.NewReader(msg),
		}
	}
}

type TCPTransport struct {
	peerChan     chan *TCPPeer
	listenAddres string
	listener     net.Listener
}

func NewTCPTransport(address string, peerChan chan *TCPPeer) *TCPTransport {
	return &TCPTransport{
		peerChan:     peerChan,
		listenAddres: address,
	}
}

func (t *TCPTransport) Start() error {
	ln, err := net.Listen("tcp", t.listenAddres)
	if err != nil {
		return err
	}
	t.listener = ln
	go t.acceptLoop()

	return nil
}

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			fmt.Printf("accept error from %+v\n", conn)
			continue
		}
		peer := &TCPPeer{
			conn: conn,
		}
		t.peerChan <- peer
	}
}
