package main

import (
	"fmt"
	"net"
	"os"
)

type client struct {
	name string
	addr *net.UDPAddr
}

func main() {
	addr, err := net.ResolveUDPAddr("udp", ":9000")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error resolving UDP address: ", err)
		os.Exit(1)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error starting server: ", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Println("Listening on port 9000...")

	clients := map[string]client{}
	buf := make([]byte, 1024)
	for {
		// Recieve message from client
		cnt, sender, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error Reading from client: ", err)
			continue
		}

		// Register username and address of unknown clients
		_, registered := clients[sender.String()]
		if !registered {
			clients[sender.String()] = client{string(buf[:cnt]), sender}
			fmt.Printf("%s(%s) joined\n",
				sender.String(),
				clients[sender.String()].name)
			continue
		}

		// Broadcast audio data to other clients
		for key, client := range clients {
			if key != sender.String() {
				conn.WriteToUDP(buf[:cnt], client.addr)
			}
		}

		// Server event logging
		//fmt.Printf("%s is talking \n", clients[sender.String()].name)
	}
}
