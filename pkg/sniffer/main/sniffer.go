package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	goflag "flag"

	rocinante "github.com/spyroot/rocinante/pkg/client"
	"github.com/spyroot/rocinante/pkg/flow"

	"github.com/golang/glog"
	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/spf13/cobra"
	"github.com/spyroot/rocinante/pkg/loadbalancer/artifacts"
)

type Sniffer struct {
	DeviceName        string
	lastReadLen       int
	readBytesConsumed int
	// go routine write to this buffered channel
	//captureReady chan<- struct{}
	quitReady chan struct{}

	timeout          time.Duration
	handle           *pcap.Handle
	packetSource     *gopacket.PacketSource
	numPktBuffer     int
	verbose          bool
	apiRestEndpoints []string
}

/**

 */
func NewSniffer(iface string, apiRestEndpoints []string, lastReadLen int, v bool, quite chan struct{}) (*Sniffer, error) {

	if len(apiRestEndpoints) == 0 {
		return nil, fmt.Errorf("api endpoint empty. check config")
	}

	s := &Sniffer{
		DeviceName:       iface,
		lastReadLen:      lastReadLen,
		quitReady:        quite,
		numPktBuffer:     100,
		verbose:          v,
		apiRestEndpoints: apiRestEndpoints,
	}

	var err error
	glog.Infof("Opening interface to capture %s", iface)
	s.handle, err = pcap.OpenLive(s.DeviceName, int32(lastReadLen), true, s.timeout)
	if err != nil {
		return nil, err
	}

	dev, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}

	ok := false
	for _, d := range dev {
		if d.Name == iface {
			ok = true
		}
	}
	if ok == false {
		return nil, fmt.Errorf("failed identify capure device")
	}

	s.packetSource = gopacket.NewPacketSource(s.handle, s.handle.LinkType())

	return s, nil
}

func (s *Sniffer) Stop() {
	s.quitReady <- struct{}{}
}

/**

 */
func (s *Sniffer) capture(captureReady chan<- map[uint64][]flow.Hdr) {

	glog.Infof("Started capturing")
	flowTable := make(map[uint64][]flow.Hdr, 0)

	numPkt := 0
	for packet := range s.packetSource.Packets() {
		if packet.TransportLayer() == nil {
			continue
		}

		hash := packet.TransportLayer().TransportFlow().FastHash()
		pkt := flow.Hdr{
			SrcPort: packet.TransportLayer().TransportFlow().Src().String(),
			DstPort: packet.TransportLayer().TransportFlow().Dst().String(),
			SrcIp:   packet.NetworkLayer().NetworkFlow().Src().String(),
			DstIp:   packet.NetworkLayer().NetworkFlow().Dst().String(),
			Proto:   packet.TransportLayer().LayerType().String(),
		}

		if s.verbose {
			glog.Infof("Flow hash %d", hash)
		}

		if f, ok := flowTable[hash]; ok {
			found := false
			for _, e := range f {
				if e.SrcIp == pkt.SrcIp && e.DstIp == pkt.DstIp {
					found = true
					break
				}
			}
			if !found {
				flowTable[hash] = append(flowTable[hash], pkt)
			}
		} else {
			frame := make([]flow.Hdr, 0)
			flowTable[hash] = append(frame, pkt)
		}

		if s.verbose {
			glog.Infof("Wrote %d packet to buffer.", numPkt)
		}
		if s.numPktBuffer == numPkt {
			glog.Infof("Capture %d packets", s.numPktBuffer)
			break
		}
		numPkt++
	}

	// in either we capture or we time out , in both case we send ready signal back
	captureReady <- flowTable
	return
}

/**

 */
func (s *Sniffer) startCapture(captureReady chan<- map[uint64][]flow.Hdr) {

	buffer := make(chan map[uint64][]flow.Hdr, 32)
	for {
		s.capture(buffer)
		select {
		case pkt := <-buffer:
			glog.Infof("Sending buffer to publisher.")
			captureReady <- pkt
		case <-s.quitReady:
			glog.Infof("Sending buffer to publisher")
			break
		}
	}
}

/**

 */
func (s *Sniffer) publish(id int, client *rocinante.RestClient, captureReady <-chan map[uint64][]flow.Hdr, publishReady chan<- struct{}) {

	if s.verbose {
		glog.Infof("Publisher")
	}

	select {
	case msg := <-captureReady:

		if s.verbose {
			glog.Infof("worker %d id Publishing", id, msg)
		} else {
			glog.Infof("worker %d id Publishing", id)
		}

		for key, val := range msg {
			buffer := &bytes.Buffer{}
			_ = gob.NewEncoder(buffer).Encode(val)
			k := strconv.FormatUint(key, 10)
			ok, err := client.Store(k, buffer.Bytes())
			if err != nil {
				glog.Infof("Failed to store value server return %v", err)
				break
			}
			if ok == false {
				glog.Infof("Failed to store", err)
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		publishReady <- struct{}{}

	case <-s.quitReady:
		glog.Infof("Publisher received a quit signal")
		return
	}

	if s.verbose {
		glog.Infof("Publisher thread %d done sending", id)
	}
}

func (s *Sniffer) start() {

	captureReady := make(chan map[uint64][]flow.Hdr, s.numPktBuffer)
	publishReady := make(chan struct{}, 32)

	// capture
	go func() {
		s.startCapture(captureReady)
	}()

	// publish to api
	for i := 0; i < 4; i++ {
		// Set up a connection to the server.
		go func(id int) {
			// rest client per each thread
			apiClient, err := rocinante.NewRestClient(s.apiRestEndpoints)
			if err != nil {
				log.Fatal(err.Error())
				return
			}

			for {
				if s.verbose {
					glog.Infof("Staring publisher %d", id)
				}
				s.publish(id, apiClient, captureReady, publishReady)

				if s.verbose {
					glog.Infof("Publisher %d, waiting for capture", id)
				}
				<-publishReady

				if s.verbose {
					glog.Infof("Published %d done", id)
				}
			}
		}(i)
	}

	glog.Infof("Main thread waiting stop")
	<-s.quitReady
}

/*

 */
func createClient(file string) ([]string, error) {

	// that just default
	var configFile = "config.yaml"
	var apiRestEndpoints []string

	if len(file) > 0 {
		configFile = file
	}

	artifact, err := artifacts.Read(configFile)
	if err != nil {
		return apiRestEndpoints, err
	}

	servers := artifact.Service.Pool.Servers
	api := artifact.Service.Pool.Api
	var farm []string
	for _, s := range servers {
		s := "http://" + s.Address + ":" + s.Port
		farm = append(farm, s)
	}

	for _, s := range api {
		s := "http://" + s.Address + ":" + s.Rest
		apiRestEndpoints = append(apiRestEndpoints, s)
	}

	return apiRestEndpoints, nil
}

// root command for capture life stream
func Capture() *cobra.Command {

	var pktsize string
	var verbose bool

	captureCmd := &cobra.Command{
		Use:   "capture",
		Short: "capture traffic on provided interface",

		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("capture require interface name (en0, eth0 ..0)")
			}

			dev, err := pcap.FindAllDevs()
			if err != nil {
				return err
			}

			ok := false
			for _, d := range dev {
				if d.Name == args[0] {
					ok = true
				}
			}
			if ok == true {
				return nil
			}

			return fmt.Errorf("invalid interface specified: %s", args[0])
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			config, err := cmd.Flags().GetString("config")
			if err != nil {
				return fmt.Errorf("provide path to config.yaml")
			}
			glog.Infof("Config set %v", config)
			if len(args) < 1 {
				return fmt.Errorf("interface name we capture")
			}
			if len(args[0]) == 0 {
				return fmt.Errorf("interface name we capture")
			}

			apiRestEndpoints, err := createClient(config)
			if err != nil {
				return fmt.Errorf("failed to read cofiguraton ")
			}

			lock.Lock()
			channelQuit = make(chan struct{})
			lock.Unlock()

			glog.Infof("rocinante api end points %v, capturing interface %s", apiRestEndpoints, args[0])
			sniffer, err := NewSniffer(args[0], apiRestEndpoints, 1600, verbose, channelQuit)
			if err != nil {
				glog.Error(err)
				return err
			}
			if sniffer == nil {
				glog.Errorf("sniffer is nil")
				return fmt.Errorf("failed create sniffer instance")
			}

			defer sniffer.Stop()
			sniffer.start()

			<-channelQuit
			return nil
		},
	}

	captureCmd.Flags().StringVarP(&pktsize, "pkt-size", "p", "1600", "packet size to capture")
	captureCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	return captureCmd
}

var (
	rootCmd = &cobra.Command{
		Long: "Use glog with cobra.",
		Run:  echo,
	}

	channelQuit chan struct{}
	sniffer     *Sniffer
	str         string
	lock        sync.Mutex
)

func init() {
	rootCmd.AddCommand(Capture())
	rootCmd.PersistentFlags().StringVar(&str, "config", "cofig.yaml", "path to config.yaml")
	//flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
}

func echo(cmd *cobra.Command, args []string) {

	//_ = goflag.Set("logtostderr", "true")
	//_ = goflag.Set("stderrthreshold", "WARNING")
	//_ = goflag.Set("v", "2")
	//
	goflag.Parse()
	glog.Info("echo (info): ", str)
}

func signalHandler() {

	if sniffer != nil {
		sniffer.Stop()
	}

	lock.Lock()
	defer lock.Unlock()
	if channelQuit != nil {
		channelQuit <- struct{}{}
	}
}

func main() {

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		signalHandler()
		os.Exit(1)
	}()

	err := rootCmd.Execute()
	if err != nil {
		glog.Error(err)
	}
}
