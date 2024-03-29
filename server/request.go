package server

import (
	"net"
	"sync"

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
	sendQuota uint16
	quotaWG   *sync.WaitGroup
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

	if r.quotaWG != nil {
		r.quotaWG.Done()
		r.quotaWG = nil
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
			log.Printf("req %d:%d onClientData, force close cause write failed:%v",
				r.idx, r.tag, err)
			ot := r.ot
			if ot != nil {
				// write failed, force close
				ot.onRequestTerminate(r)
			}
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

	buf := make([]byte, 8192)
	for {
		n, err := c.Read(buf)

		if !r.isUsed {
			// request is free!
			log.Printf("req %d:%d  read, request is free, discard data:%d",
				r.idx, r.tag, n)
			break
		}

		ot := r.ot
		if ot == nil {
			log.Printf("req %d:%d  read, request'tunnel is free, discard data:%d",
				r.idx, r.tag, n)
			break
		}

		if err != nil {
			// log.Println("proxy read failed:", err)
			ot.onRequestTerminate(r)
			break
		}

		// golang will never return n == 0
		if n == 0 {
			// log.Println("proxy read, server half close")
			ot.onRequestHalfClosed(r)
			break
		}

		t := r.owner.getTunnelForData()
		if t == nil {
			log.Printf("req %d:%d read, getTunnel nil, discard data", r.idx, r.tag)
			ot.onRequestHalfClosed(r)
			break
		}

		// log.Println("proxy n:", n)

		t.onRequestData(r, buf[:n])
		r.sendQuota--

		if r.sendQuota == 0 {
			//log.Println("proxy, wait quota")
			r.quotaWG = &sync.WaitGroup{}
			r.quotaWG.Add(1)
			r.quotaWG.Wait()
			//log.Println("proxy, wait quota complete")
		}
	}
}

func (r *Request) updateQuota(quota uint16) {
	needNotify := r.sendQuota == 0
	r.sendQuota = r.sendQuota + quota

	if needNotify && r.quotaWG != nil {
		//log.Println("updateQuota:", quota)
		r.quotaWG.Done()
		r.quotaWG = nil
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
