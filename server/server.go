package server

import (
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"net/http"
)

const (
	defaultTunCap        = 4
	defaultAccountReqCap = 200
)

var (
	upgrader   = websocket.Upgrader{} // use default options
	wsIndex    = 0
	accountMap = make(map[string]*Account)
)

func wsHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	var uuid = r.URL.Query().Get("uuid")
	if uuid == "" {
		log.Println("need uuid!")
		return
	}

	account, ok := accountMap[uuid]
	if !ok {
		log.Println("no account found for uuid:", uuid)
		return
	}

	account.acceptWebsocket(c)
}

func keepalive() {
	for {
		time.Sleep(time.Second * 30)

		for _, a := range accountMap {
			a.keepalive()
		}
	}
}

func setupBuiltinAccount() {

	uuids := []string{
		"ee80e87b-fc41-4e59-a722-7c3fee039cb4",
		"f6000866-1b89-4ab4-b1ce-6b7625b8259a"}

	for _, u := range uuids {
		accountMap[u] = newAccount(defaultAccountReqCap, defaultTunCap, u)
	}
}

// CreateHTTPServer start http server
func CreateHTTPServer(listenAddr string, wsPath string) {
	setupBuiltinAccount()

	go keepalive()
	http.HandleFunc(wsPath, wsHandler)
	log.Printf("server listen at:%s, path:%s", listenAddr, wsPath)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
