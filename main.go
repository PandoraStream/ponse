package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

var config *tls.Config
var serverAddress string
var serverPort string
var disableTLS bool

func main() {
	log.SetFlags(log.Lshortfile)

	err := godotenv.Load()
	if err != nil {
		log.Fatalln(err)
		return
	}

	disableTLS = len(os.Getenv("PONSE_DISABLE_TLS")) > 0
	var cer tls.Certificate
	if !disableTLS {
		cer, err = tls.LoadX509KeyPair("server.crt", "server.key")
		if err != nil {
			log.Fatalln(err)
			return
		}
	}

	config = &tls.Config{
		MinVersion: tls.VersionTLS10, // The 3DS uses TLS 1.0 when doing handshake
		InsecureSkipVerify: true,
	}

	if !disableTLS {
		config.Certificates = []tls.Certificate{cer}
	}

	// Read the iRTSP destination address from the PONSE_SERVER_URI env. This can be timed
	// with an HTTP(S) proxy to get the address before starting the proxy. Example:
	// irtsp://140.227.187.170:41002
	address := os.Getenv("PONSE_SERVER_URI")
	filteredAddress, _ := strings.CutPrefix(address, "irtsp://")
	serverAddress, serverPort, _ = strings.Cut(filteredAddress, ":")

	ln, err := net.Listen("tcp", ":" + serverPort)
	if err != nil {
		log.Println(err)
		return
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go handleIRTSPConnection(conn)
	}
}

func handleIRTSPConnection(conn net.Conn) {
	defer conn.Close()
	serverConn, err := net.Dial("tcp", serverAddress + ":" + serverPort)
	if err != nil {
		log.Println(err)
		return
	}
	defer serverConn.Close()
	for {
		buffer := make([]byte, 1024)

		// TODO - With this hack we change between client->server and server->client messages faster
		// when doing everything on the same goroutine. Split interactions into separate goroutines
		// and make TLS not break in the process
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buffer)
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			log.Println(n, err)
			break
		}
		buffer = buffer[:n]

		if len(buffer) > 0 {
			req := NewMessage(buffer)
			log.Printf("%+v\n", req)

			n, err = serverConn.Write(req.ToBytes())
			if err != nil {
				log.Println(n, err)
				break
			}

			// The client can also send response messages, so we check the message type for logging
			var messageType string
			if req.Code > 0 {
				messageType = "response"
			} else {
				messageType = "request"
			}

			log.Printf("[CLIENT] iRTSP %s:\n", messageType)
			fmt.Printf("%s\n", req.ToBytes())
		}

		serverConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		buffer = make([]byte, 1024)
		n, err = serverConn.Read(buffer)
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			log.Println(n, err)
			break
		}
		buffer = buffer[:n]

		if len(buffer) > 0 {
			res := NewMessage(buffer)
			log.Printf("%+v\n", res)

			// When we receive the stream media ports, start a connection on those ports
			// for proxying the data
			if res.Method == "SETUP" {
				videoHeader := res.Headers["v"]
				startMediaConnection(videoHeader, "VIDEO")
				audioHeader := res.Headers["a"]
				// TODO - Is this even possible?
				if audioHeader != videoHeader {
					startMediaConnection(audioHeader, "AUDIO")
				}
				controlHeader := res.Headers["c"]
				if controlHeader != videoHeader && controlHeader != audioHeader {
					startMediaConnection(controlHeader, "CONTROL")
				}
			}

			// When we receive the KNOCK port, start a connection on it for proxying
			// the data
			// The KNOCK header looks like this:
			// iDataChunk/unicast/tcp/40605;
			// So we trim the ; at the end
			if res.Method == "KNOCK" {
				knockHeader := res.Headers["p"]
				startMediaConnection(strings.TrimRight(knockHeader, ";"), "KNOCK")
			}

			if res.Method == "START" && disableTLS {
				// The server controls whether the client should do a TLS handshake
				// with the "scheme" header
				// Disable TLS on the client by clearing out the header
				if scheme, ok := res.Headers["sc"]; ok && scheme == "tls" {
					res.Headers["sc"] = ""
				}
			}

			n, err = conn.Write(res.ToBytes())
			if err != nil {
				log.Println(n, err)
				break
			}

			// The server can also send request messages, so we check the message type for logging
			var messageType string
			if res.Code > 0 {
				messageType = "response"
			} else {
				messageType = "request"
			}

			log.Printf("[SERVER] iRTSP %s:\n", messageType)
			fmt.Printf("%s\n", res.ToBytes())

			// When we receive the START response from the server, do the TLS handshake.
			// TODO - This assumes that the server wants a TLS handshake
			if res.Method == "START" {
				if !disableTLS {
					conn = tls.Server(conn, config)
				}
				serverConn = tls.Client(serverConn, config)
			}
		}
	}
}

func startMediaConnection(header, kind string) {
	// A media header consists of 4 sections:
	// iDataChunk/unicast/tcp/40603
	// 1. The streaming type: "iDataChunk"
	// 2. The delivery type: "unicast" (or "multicast"?)
	// 3. The transmission protocol used: "tcp" or "ust"
	// 4. The server port: "40603"
	headerStrings := strings.Split(header, "/")
	port := headerStrings[len(headerStrings)-1] // Extract the port from the last section
	network := headerStrings[len(headerStrings)-2] // Extract the network from the third section

	// UST is a custom network protocol over UDP. It is used as a "slow connection" mode,
	// but the UST payload is the same as in TCP mode
	if network == "ust" {
		network = "udp"
		portInt, err := strconv.Atoi(port)
		if err != nil {
			log.Println(err)
			return
		}

		conn, err := net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: portInt})
		if err != nil {
			log.Println(err)
			return
		}

		go handleMediaConnection(conn, network, port, kind)
		return
	}

	ln, err := net.Listen(network, ":" + port)
	if err != nil {
		log.Println(err)
		return
	}

	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Println(err)
				continue
			}
			go handleMediaConnection(conn, network, port, kind)
		}
	}()
}

func handleMediaConnection(conn net.Conn, network, port, kind string) {
	serverConn, err := net.Dial(network, serverAddress + ":" + port)
	if err != nil {
		log.Println(err)
		return
	}

	defer serverConn.Close()
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go func(wg *sync.WaitGroup) {
		for {
			buffer := make([]byte, 1024)
			n, err := conn.Read(buffer)
			if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
				log.Println(n, err)
				break
			}
			buffer = buffer[:n]

			if len(buffer) > 0 {
				// TODO - Investigate why UDP isn't working
				if network == "udp" {
					n, err = conn.(*net.UDPConn).WriteTo(buffer, serverConn.RemoteAddr())
				} else {
					n, err = serverConn.Write(buffer)
				}
				if err != nil {
					log.Println(n, err)
					break
				}

				log.Printf("[%s] Media request:\n", kind)
				// fmt.Printf("%x\n", buffer)
			}
		}
		wg.Done()
	}(wg)
	go func(wg *sync.WaitGroup) {
		for {
			buffer := make([]byte, 1024)
			n, err := serverConn.Read(buffer)
			if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
				log.Println(n, err)
				break
			}
			buffer = buffer[:n]

			if len(buffer) > 0 {
				// TODO - Investigate why UDP isn't working
				if network == "udp" {
					n, err = serverConn.(*net.UDPConn).WriteTo(buffer, conn.RemoteAddr())
				} else {
					n, err = conn.Write(buffer)
				}
				if err != nil {
					log.Println(n, err)
					break
				}

				log.Printf("[%s] Media response:\n", kind)
				// fmt.Printf("%x\n", buffer)
			}
		}
		wg.Done()
	}(wg)
	wg.Wait()
}
