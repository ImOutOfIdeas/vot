package main

import (
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gordonklaus/portaudio"
)

// measure noise floor from first few callbacks
var noise_floor float64
var calibration_frames, hold_frames uint8

func rms(in []int32) float64 {
	var sum float64
	for _, sample := range in {
		value := float64(sample)
		sum += value * value
	}
	return math.Sqrt(sum / float64(len(in)))
}

func select_device(devices []*portaudio.DeviceInfo) *portaudio.DeviceInfo {
	for i, d := range devices {
		fmt.Printf("%d: %s\n", i, d.Name)
	}
	var choice int
	fmt.Scan(&choice)
	return devices[choice]
}

func connect_to_server(address string) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	return net.DialUDP("udp", nil, addr)
}

func register_username(conn *net.UDPConn) {
	fmt.Print("Enter username: ")
	var username string
	fmt.Scanln(&username)
	conn.Write([]byte(username))
}

func setup_devices() (*portaudio.DeviceInfo, *portaudio.DeviceInfo) {
	devices, _ := portaudio.Devices()

	fmt.Print("\033[H\033[2J")
	fmt.Println("=== Select Input Device ===")
	idevice := select_device(devices)

	fmt.Print("\033[H\033[2J")
	fmt.Println("=== Select Output Device ===")
	odevice := select_device(devices)

	return idevice, odevice
}

func make_input_params(device *portaudio.DeviceInfo) portaudio.StreamParameters {
	return portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: 1,
			Latency:  device.DefaultLowInputLatency,
		},
		SampleRate:      48000,
		FramesPerBuffer: 1024,
	}
}

func make_output_params(device *portaudio.DeviceInfo) portaudio.StreamParameters {
	return portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: 1,
			Latency:  device.DefaultLowOutputLatency,
		},
		SampleRate:      48000,
		FramesPerBuffer: 1024,
	}
}

func open_input_stream(params portaudio.StreamParameters, conn *net.UDPConn) (*portaudio.Stream, error) {
	return portaudio.OpenStream(params, func(in []int32) {
		level := rms(in)

		// Calibrate noise floor
		if calibration_frames < 50 {
			// Running average of starting input level
			noise_floor = (noise_floor*float64(calibration_frames) + level) / float64(calibration_frames+1)
			calibration_frames++
			return
		}
		// Add holdover time when speaking starts
		if level > noise_floor*3 {
			hold_frames = 20
		}
		if hold_frames == 0 {
			return
		}
		hold_frames--

		fmt.Println(level)

		// Send samples to server as byte array
		bytes := make([]byte, len(in)*4)
		for i, sample := range in {
			bytes[i*4] = byte(sample)
			bytes[i*4+1] = byte(sample >> 8)
			bytes[i*4+2] = byte(sample >> 16)
			bytes[i*4+3] = byte(sample >> 24)
		}
		conn.Write(bytes)
	})
}

func open_output_stream(params portaudio.StreamParameters, output_channel chan []int32) (*portaudio.Stream, error) {
	// Fills output buffer with data from output_channel
	return portaudio.OpenStream(params, func(out []int32) {
		select {
		case samples := <-output_channel:
			copy(out, samples)
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
}

func receive_audio(conn *net.UDPConn, output_channel chan []int32, sig chan os.Signal) {
	// Receive UDP packets and push samples into the output_channel
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
}

func main() {
	// Setup SIGINT and SIGTERM signal channel for graceful exit
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Connect to UDP server
	conn, err := connect_to_server("100.113.183.17:9000")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error connecting to UDP server: ", err)
		return
	}
	defer conn.Close()

	register_username(conn)

	// Setup portaudio
	err = portaudio.Initialize()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error initializing portaudio: ", err)
		return
	}
	defer portaudio.Terminate()

	idevice, odevice := setup_devices()

	// Create input stream with capture callback closure
	istream, err := open_input_stream(make_input_params(idevice), conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error opening input stream: ", err)
		return
	}
	istream.Start()
	defer istream.Stop()

	// Create channel for samples received from the server
	output_channel := make(chan []int32, 10)

	// Create output stream with playback callback closure
	ostream, err := open_output_stream(make_output_params(odevice), output_channel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error opening output stream: ", err)
		return
	}
	ostream.Start()
	defer ostream.Stop()

	go receive_audio(conn, output_channel, sig)

	fmt.Print("\033[H\033[2J")
	fmt.Println("*** Have Fun and Be Nice! ***")

	// Catch SIGINT and SIGTERM to run defer statements and exit cleanly
	<-sig
}
