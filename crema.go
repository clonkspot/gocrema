package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"regexp"

	"github.com/lluchs/gocrema/eventsource"
)

// GameEventsURL is the URL to the league event stream.
var GameEventsURL = "https://clonkspot.org/league/game_events.php"

// LeagueURL is the URL to the league server.
var LeagueURL = "https://clonkspot.org/league/league.php"

func getGameAddresses(id int) ([]net.Addr, error) {
	url := fmt.Sprintf("%s?action=query&game_id=%d", LeagueURL, id)
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	linere := regexp.MustCompile(`(?m)^Address=(.+)$`)
	addressre := regexp.MustCompile(`(TCP|UDP):"?([0-9a-f:.[\]]+)"?`)
	m := linere.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("No Address= line in league answer")
	}
	addrm := addressre.FindAllSubmatch(m[1], -1)
	addrs := make([]net.Addr, len(addrm))
	for i, addr := range addrm {
		switch string(addr[1]) {
		case "TCP":
			addrs[i], err = net.ResolveTCPAddr("tcp", string(addr[2]))
		case "UDP":
			addrs[i], err = net.ResolveUDPAddr("udp", string(addr[2]))
		default:
			err = fmt.Errorf("unexpected network %s", addr[1])
		}
		if err != nil {
			return nil, err
		}
	}
	// TODO: Also retrieve netpuncher info
	return addrs, nil
}

// printAddrStatus prints the connection status for all addresses of game #id.
func printAddrStatus(cache *Cache, id int) {
	addrs, err := getGameAddresses(id)
	if err != nil {
		fmt.Printf("error getting addresses: %v\n", err)
		return
	}
	fmt.Printf("game %d\n", id)
	for _, addr := range addrs {
		if shouldSkipAddr(addr) {
			continue
		}
		fmt.Printf(" - %s:%s -> %s\n", addr.Network(), addr, cache.Get(CacheRequest{ID: id, Addr: addr}))
	}
}

func main() {
	es := eventsource.New(GameEventsURL)
	defer es.Close()

	cache := NewCache()

	for {
		select {
		case <-es.OnOpen:
			fmt.Println("open")
		case msg := <-es.OnMessage:
			switch msg.EventType {
			case "init":
				var games []LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &games); err != nil {
					fmt.Println("error parsing JSON", err)
					break
				}
				fmt.Printf("init with %d games\n", len(games))
				for _, game := range games {
					printAddrStatus(cache, game.ID)
				}
			case "create":
				fallthrough
			case "update":
				var game LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &game); err != nil {
					fmt.Println("error parsing JSON", err)
					break
				}
				fmt.Printf("event %s: %+v\n", msg.EventType, game)
				printAddrStatus(cache, game.ID)
			default:
				fmt.Println(msg.EventType, msg.Data)
			}
		case err := <-es.OnError:
			fmt.Printf("err: %v\n", err)
		}
	}
}
