package grpc

import (
	"encoding/json"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"istio.io/api/networking/v1alpha3"
	istiolog "istio.io/pkg/log"
)

var (
	log = istiolog.RegisterScope("sdp", "sdp debugging", 0)

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

const (
	SDP_LABEL       = "sdp"
	EnvoyFilterType = "networking.istio.io/v1alpha3/envoy.filter"
)

type EnvoyFilters struct {
	EnvoyFilters []*v1alpha3.EnvoyFilter
}

// DiscoveryStream is a server interface for XDS.
type DiscoveryStream = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer

// DeltaDiscoveryStream is a server interface for Delta XDS.
type DeltaDiscoveryStream = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer

// DiscoveryClient is a client interface for XDS.
type DiscoveryClient = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// DeltaDiscoveryClient is a client interface for Delta XDS.
type DeltaDiscoveryClient = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient

func nonce() string {
	return uuid.New().String()
}

type BuildInfo struct {
	Version       string `json:"version"`
	GitRevision   string `json:"revision"`
	GolangVersion string `json:"golang_version"`
	BuildStatus   string `json:"status"`
	GitTag        string `json:"tag"`
}

// IstioControlPlaneInstance defines the format Istio uses for when creating Envoy config.core.v3.ControlPlane.identifier
type IstioControlPlaneInstance struct {
	// The Istio component type (e.g. "istiod")
	Component string
	// The ID of the component instance
	ID string
	// The Istio version
	Info BuildInfo
}

// control_plane:{identifier:"{\"Component\":\"istiod\",\"ID\":\"\",\"Info\":{\"version\":\"unknown\",\"revision\":\"unknown\",\"golang_version\":\"go1.19.2\",\"status\":\"unknown\",\"tag\":\"unknown\"}}"}
func makeContrelPlane() *core.ControlPlane {
	byVersion, err := json.Marshal(IstioControlPlaneInstance{
		Component: "sdp",
		ID:        "synapse-discovery-proxy",
		Info: BuildInfo{
			Version:       "unknown",
			GitRevision:   "unknown",
			GolangVersion: "go1.19.2",
			BuildStatus:   "unknown",
			GitTag:        "unknown",
		},
	})

	if err != nil {
		log.Errorf("XDS: Could not serialize control plane id: %v", err)
	}

	return &core.ControlPlane{Identifier: string(byVersion)}
}
