package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/apex/log"
	"github.com/apex/log/handlers/cli"
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

	// Netpuncher info
	npaddrre := regexp.MustCompile(`(?m)^NetpuncherAddr="([^"]+)"$`)
	m = npaddrre.FindSubmatch(body)
	if m != nil {
		netpuncherAddr := string(m[1])
		// Just match the IPv4/IPv6 lines, don't bother with the exact format
		npre := regexp.MustCompile(`(?m)^ +IPv([46])=([0-9]+)$`)
		ms := npre.FindAllSubmatch(body, -1)
		for _, m := range ms {
			id, _ := strconv.ParseUint(string(m[2]), 10, 64)
			addrs = append(addrs, &NetpuncherAddr{Net: "netpuncher" + string(m[1]), Addr: netpuncherAddr, ID: id})
		}
	}

	return addrs, nil
}

func main() {
	log.SetHandler(cli.Default)

	cache := NewCache()

	go monitorGames(cache)

	for {
		time.Sleep(10 * time.Second)
		games := cache.Get()
		for _, g := range games {
			fmt.Printf("%s on %s (#%d)\n", g.Game.Title, g.Game.Host, g.Game.ID)
			for key, addr := range g.Addrs {
				fmt.Printf(" - %s: %s\n", key, addr.Status)
			}
			fmt.Println()
		}
	}
}

func monitorGames(c *Cache) {
	es := eventsource.New(GameEventsURL)
	defer es.Close()

	for {
		select {
		case <-es.OnOpen:
			// do nothing
		case msg := <-es.OnMessage:
			switch msg.EventType {
			case "init":
				var games []LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &games); err != nil {
					log.WithError(err).Error("init: error parsing JSON")
					break
				}
				log.Infof("init with %d games\n", len(games))
				c.UpdateAllGames(games)
				for _, game := range games {
					addrs, err := getGameAddresses(game.ID)
					if err != nil {
						log.WithError(err).WithField("id", game.ID).Error("init: error getting addresses")
						continue
					}
					c.UpdateAddrs(game.ID, addrs)
				}
			case "create", "update":
				var game LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &game); err != nil {
					log.WithError(err).Error("create/update: error parsing JSON")
					break
				}
				addrs, err := getGameAddresses(game.ID)
				if err != nil {
					log.WithError(err).WithField("id", game.ID).Error("create/update: error getting addresses")
					break
				}
				c.UpdateAddrs(game.ID, addrs)
			case "end", "delete":
				var game LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &game); err != nil {
					log.WithError(err).Error("end/delete: error parsing JSON")
					break
				}
				c.DeleteGame(game.ID)
			default:
				fmt.Println(msg.EventType, msg.Data)
			}
		case err := <-es.OnError:
			fmt.Printf("err: %v\n", err)
		}
	}
}
