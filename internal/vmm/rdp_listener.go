package vmm

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
	"golang.org/x/sys/unix"
)

func (v *VMM) newRDPDisplay() (*virtio.RDPDisplay, error) {
	var config *tls.Config
	if v.RDPCert != "" || v.RDPKey != "" {
		certificate, err := tls.LoadX509KeyPair(v.RDPCert, v.RDPKey)
		if err != nil {
			return nil, fmt.Errorf("RDP certificate: %w", err)
		}
		config = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	}
	var listener net.Listener
	if v.RDPListenerFile != nil {
		var err error
		listener, err = v.inheritedRDPListener()
		if err != nil {
			return nil, err
		}
	}
	display, err := virtio.NewRDPDisplayWithConfig(v.RDP, virtio.RDPConfig{
		TLS: config, Listener: listener, H264: v.RDPH264, Audio: v.Audio == "rdp", Profile: v.RDPProfile,
		H264Encoder: v.RDPH264Encoder,
		H264Threads: v.RDPH264Threads, Stats: v.RDPStats, GuestCPUs: v.NCPUs,
	})
	if err != nil && listener != nil {
		err = errors.Join(err, listener.Close())
	}

	return display, err
}

func (v *VMM) inheritedRDPListener() (net.Listener, error) {
	// FileListener also accepts connected TCP sockets, whose Accept calls fail.
	accepting, err := unix.GetsockoptInt(int(v.RDPListenerFile.Fd()), unix.SOL_SOCKET, unix.SO_ACCEPTCONN)
	if err != nil {
		return nil, fmt.Errorf("adopt inherited RDP listener: %w", err)
	}
	if accepting != 1 {
		return nil, errRDPListenerConfig
	}
	// FileListener duplicates the descriptor. Closing a display must not close
	// the original inherited socket needed by the next firmware boot.
	listener, err := net.FileListener(v.RDPListenerFile)
	if err != nil {
		return nil, fmt.Errorf("adopt inherited RDP listener: %w", err)
	}
	if _, ok := listener.(*net.TCPListener); !ok || listener.Addr().String() != v.RDP {
		return nil, errors.Join(errRDPListenerConfig, listener.Close())
	}

	return listener, nil
}
