package server

import (
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// Account account
type Account struct {
	uuid          string
	tunnels       []*Tunnel
	nextTunnelIdx int

	reqq *Reqq
}

func newAccount(reqCap int, tunCap int, uuid string) *Account {
	a := &Account{
		uuid:    uuid,
		tunnels: make([]*Tunnel, tunCap),
	}

	reqq := newReqq(reqCap, a)
	a.reqq = reqq

	return a
}

func (a *Account) acceptWebsocket(conn *websocket.Conn) {
	log.Printf("account:%s accept websocket, total:%d", a.uuid, 1+len(a.tunnels))
	var idx int
	var found bool
	for i, t := range a.tunnels {
		if t == nil {
			idx = i
			found = true
		}
	}

	if !found {
		log.Printf("account:%s accept websocket, failed, no valid tunnel slot", a.uuid)
		return
	}

	tun := newTunnel(idx, conn, a)
	a.tunnels[idx] = tun
	defer func() {
		a.tunnels[idx] = nil
	}()

	tun.serve()
}

func (a *Account) keepalive() {
	for _, t := range a.tunnels {
		if t == nil {
			continue
		}

		t.keepalive()
	}
}

func (a *Account) getTunnelForData() *Tunnel {
	idx := a.nextTunnelIdx
	for i := idx; i < len(a.tunnels); i++ {
		t := a.tunnels[i]
		if t == nil || t.conn == nil {
			continue
		}

		a.nextTunnelIdx = (i + 1) % len(a.tunnels)
		return t
	}

	for i := 0; i < idx; i++ {
		t := a.tunnels[i]
		if t == nil || t.conn == nil {
			continue
		}

		a.nextTunnelIdx = (i + 1) % len(a.tunnels)
		return t
	}

	return nil
}
