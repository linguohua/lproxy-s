package server

import (
	"net"

	log "github.com/sirupsen/logrus"
)

// Request request
type Request struct {
	isUsed bool
	idx    uint16
	tag    uint16
	owner  *Account
	ot     *Tunnel

	conn *net.TCPConn

	sendSeqNo uint32
}

func newRequest(o *Account, idx uint16) *Request {
	r := &Request{owner: o, idx: idx}

	return r
}

func (r *Request) dofree() {
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

func (r *Request) onClientFinished() {
	if r.conn != nil {
		r.conn.CloseWrite()
	}
}

func (r *Request) onClientData(data []byte) {
	if r.conn != nil {
		err := writeAll(data, r.conn)
		if err != nil {
			log.Println("onClientData, write failed:", err)
		} else {
			// log.Println("onClientData, write:", len(data))
		}
	}
}

func (r *Request) proxy() {
	c := r.conn
	if c == nil {
		return
	}

	if !r.isUsed {
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)

		if !r.isUsed {
			// request is free!
			log.Println("proxy read, request is free, discard data:", n)
			break
		}

		ot := r.ot
		if ot == nil {
			log.Println("proxy read, request'tunnel is free, discard data:", n)
		}

		if err != nil {
			// log.Println("proxy read failed:", err)
			ot.onRequestTerminate(r)
			break
		}

		if n == 0 {
			// log.Println("proxy read, server half close")
			ot.onRequestHalfClosed(r)
			break
		}

		t := r.owner.getTunnelForData()
		if t == nil {
			log.Println("proxy read, getTunnel nil, discard data")
			break
		}

		t.onRequestData(r, buf[:n])
	}
}

func writeAll(buf []byte, nc net.Conn) error {
	wrote := 0
	l := len(buf)
	for {
		n, err := nc.Write(buf[wrote:])
		if err != nil {
			return err
		}

		wrote = wrote + n
		if wrote == l {
			break
		}
	}

	return nil
}
