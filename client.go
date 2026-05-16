package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gordonklaus/portaudio"
)

func select_device(devices []*portaudio.DeviceInfo) *portaudio.DeviceInfo {
	for i, d := range devices {
		fmt.Printf("%d: %s\n", i, d.Name)
	}
	var choice int
	fmt.Scan(&choice)
	return devices[choice]
}

func main() {
	// Setup SIGINT and SIGTERM signal channel for graceful exit
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Connect to UDP server
	addr, err := net.ResolveUDPAddr("udp", "100.113.183.17:9000")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error resolving UDP address: ", err)
		return
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error connecting to UDP server: ", err)
		return
	}
	defer conn.Close()

	// Get username input and forward to server for registration
	fmt.Print("Enter username: ")
	var username string
	fmt.Scanln(&username)
	conn.Write([]byte(username))

	// Setup portaudio
	err = portaudio.Initialize()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error initializing portaudio: ", err)
		return
	}
	defer portaudio.Terminate()


	// Setup audio devices
	devices, _ := portaudio.Devices()
	fmt.Print("\033[H\033[2J")
	fmt.Println("=== Select Input Device ===")
	idevice := select_device(devices)

	fmt.Print("\033[H\033[2J")
	fmt.Println("=== Select Output Device ===")
	odevice := select_device(devices)

	// Setup stream parameters
	iparams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   idevice,
			Channels: 1,
			Latency:  idevice.DefaultLowInputLatency,
		},
		SampleRate:      48000,
		FramesPerBuffer: 1024,
	}
	oparams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   odevice,
			Channels: 1,
			Latency:  odevice.DefaultLowOutputLatency,
		},
		SampleRate:      48000,
		FramesPerBuffer: 1024,
	}

	// Create input stream with capture callback closure
	istream, err := portaudio.OpenStream(iparams,
		// Arrange samples into little endian byte array and send to server
		func(in []int32) {
			bytes := make([]byte, len(in)*4)
			for i, sample := range in {
				bytes[i*4] = byte(sample)
				bytes[i*4+1] = byte(sample >> 8)
				bytes[i*4+2] = byte(sample >> 16)
				bytes[i*4+3] = byte(sample >> 24)
			}
			conn.Write(bytes)
		})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error opening input stream: ", err)
		return
	}
	istream.Start()
	defer istream.Stop()

	// Create channel for samples received from the server
	output_channel := make(chan []int32, 10)

	// Create output stream with playback callback closure
	ostream, err := portaudio.OpenStream(oparams,
		// Fills output buffer with data from output_channel
		func(out []int32) {
			select {
			case samples := <-output_channel:
				copy(out, samples)
			default:
				for i := range out {
					out[i] = 0
				}
			}
		})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error opening output stream: ", err)
		return
	}
	ostream.Start()
	defer ostream.Stop()

	// Receive UDP packets and push samples into the output_channel
	go func() {
		bytes := make([]byte, 1024*4)
		for {
			cnt, err := conn.Read(bytes)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error Reading from server: ", err)
				sig <- syscall.SIGTERM
				return
			}
			samples := make([]int32, cnt/4)
			for i := range samples {
				samples[i] = int32(bytes[i*4]) |
					int32(bytes[i*4+1])<<8 |
					int32(bytes[i*4+2])<<16 |
					int32(bytes[i*4+3])<<24
			}
			output_channel <- samples
		}
	}()

	fmt.Print("\033[H\033[2J")
	fmt.Println("*** Have Fun and Be Nice! ***")

	// Catch SIGINT and SIGTERM to run defer statements and exit cleanly
	<-sig
}
