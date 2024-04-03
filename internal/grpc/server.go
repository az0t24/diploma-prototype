package grpc

import (
	efwrapperv1 "diploma-prototype/api/efwrapper/v1"
	"diploma-prototype/internal/settings"
	dp_grpc "diploma-prototype/pkg/grpc"
	dp_model "diploma-prototype/pkg/model"
	"fmt"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type DiscoveryService struct {
	// AdsClients reflect active gRPC channels, for both ADS and EDS.
	AdsClients      map[string]*Connection
	adsClientsMutex sync.RWMutex

	// pushChannel is the buffer used for debouncing.
	// after debouncing the pushRequest will be sent to pushQueue
	pushChannel chan *EnvoyFilters

	// mutex used for protecting Environment.PushContext
	updateMutex sync.RWMutex
	//server добавить в этот кэш аппенд
	//триггерим - записываем pushChannel
	cache *EnvoyFilters

	CacheMap map[string]*v1alpha3.EnvoyFilter

	// ClusterAliases are aliase names for cluster. When a proxy connects with a cluster ID
	// and if it has a different alias we should use that a cluster ID for proxy.
	ClusterAliases map[cluster.ID]cluster.ID

	ControlPlane *core.ControlPlane

	updatesChan chan *efwrapperv1.Configuration
	grpcPort    int
}

// NewService creates Synapse Discovery Server that sources data from Pilot's internal mesh data structures
func NewService(updatesChan chan *efwrapperv1.Configuration, s *settings.Settings) *DiscoveryService {
	out := &DiscoveryService{
		pushChannel:  make(chan *EnvoyFilters, 1),
		AdsClients:   map[string]*Connection{},
		ControlPlane: makeContrelPlane(),
		CacheMap:     make(map[string]*v1alpha3.EnvoyFilter),
		updatesChan:  updatesChan,
		grpcPort:     s.GrpcPort,
	}

	go out.updatesHandler()

	return out
}

func (s *DiscoveryService) Run() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.grpcPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	server := grpc.NewServer()
	discovery.RegisterAggregatedDiscoveryServiceServer(server, s)
	log.Infof("server listening at %v", lis.Addr())
	if err := server.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (s *DiscoveryService) StreamAggregatedResources(stream DiscoveryStream) error {
	return s.handleStream(stream)
}

func (s *DiscoveryService) DeltaAggregatedResources(DeltaDiscoveryStream) error {
	return nil
}

func (s *DiscoveryService) handleStream(stream DiscoveryStream) error {
	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}

	con := newConnection(peerAddr, stream)

	go s.receive(con)

	// Wait for the proxy to be fully initialized before we start serving traffic. Because
	// initialization doesn't have dependencies that will block, there is no need to add any timeout
	// here. Prior to this explicit wait, we were implicitly waiting by receive() not sending to
	// reqChannel and the connection not being enqueued for pushes to pushChannel until the
	// initialization is complete.
	<-con.initialized

	log.Infof("con %s initialized", con.conID)

	for {
		select {
		case req, ok := <-con.reqChan:
			if ok {
				if err := s.processRequest(req, con); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.errorChan
			}
		case <-con.PushChannel:
			err := s.pushConnection(con)
			if err != nil {
				return err
			}
		case <-con.stop:
			return nil
		}
	}
}

func (s *DiscoveryService) receive(con *Connection) {
	defer func() {
		close(con.errorChan)
		close(con.reqChan)
		// Close the initialized channel, if its not already closed, to prevent blocking the stream.
		select {
		case <-con.initialized:
		default:
			close(con.initialized)
		}
	}()

	firstRequest := true
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if dp_grpc.IsExpectedGRPCError(err) {
				log.Infof("ADS: %q %s terminated", con.peerAddr, con.conID)
				return
			}
			con.errorChan <- err
			log.Errorf("ADS: %q %s terminated with error: %v", con.peerAddr, con.conID, err)
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstRequest {
			firstRequest = false
			if req.Node == nil || req.Node.Id == "" {
				con.errorChan <- status.New(codes.InvalidArgument, "missing node information").Err()
				return
			}
			if err := s.initConnection(req.Node, con); err != nil {
				con.errorChan <- err
				return
			}
			defer s.closeConnection(con)
			log.Infof("ADS: new connection for node:%s", con.conID)
		}

		select {
		case con.reqChan <- req:
		case <-con.stream.Context().Done():
			log.Infof("ADS: %q %s terminated with stream closed", con.peerAddr, con.conID)
			return
		}
	}
}

func (s *DiscoveryService) processRequest(req *discovery.DiscoveryRequest, con *Connection) error {
	res, err := s.generateData(con)
	if err != nil {
		log.Errorf("generating data returned with error: %s", err)
		return err
	}

	if res == nil {
		return nil
	}

	resp := &discovery.DiscoveryResponse{
		ControlPlane: s.ControlPlane,
		TypeUrl:      resource.AnyType,
		VersionInfo:  "1",
		Nonce:        nonce(),
		Resources:    dp_model.EnvoyFiltersToAny(res),
	}

	if err := con.send(resp); err != nil {
		log.Errorf("error while sending resp to %s", con.peerAddr)
		return err
	}

	return nil
}

// update the node associated with the connection, after receiving a packet from envoy, also adds the connection
// to the tracking map.
func (s *DiscoveryService) initConnection(node *core.Node, con *Connection) error {
	// Setup the initial proxy metadata
	proxy, err := s.initProxyMetadata(node)
	if err != nil {
		return err
	}
	// Check if proxy cluster has an alias configured, if yes use that as cluster ID for this proxy.
	if alias, exists := s.ClusterAliases[proxy.Metadata.ClusterID]; exists {
		proxy.Metadata.ClusterID = alias
	}
	// To ensure push context is monotonically increasing, setup LastPushContext before we addCon. This
	// way only new push contexts will be registered for this proxy.

	// First request so initialize connection id and start tracking it.
	con.conID = connectionID(proxy.ID)
	con.node = node
	con.proxy = proxy

	// Register the connection. this allows pushes to be triggered for the proxy. Note: the timing of
	// this and initializeProxy important. While registering for pushes *after* initialization is complete seems like
	// a better choice, it introduces a race condition; If we complete initialization of a new push
	// context between initializeProxy and addCon, we would not get any pushes triggered for the new
	// push context, leading the proxy to have a stale state until the next full push.
	s.addCon(con.conID, con)
	// Register that initialization is complete. This triggers to calls that it is safe to access the
	// proxy
	defer close(con.initialized)

	// Complete full initialization of the proxy
	//if err := s.initializeProxy(con); err != nil {
	//	s.closeConnection(con)
	//	return err
	//}

	return nil
}

func (s *DiscoveryService) closeConnection(con *Connection) {
	if con.conID == "" {
		return
	}
	s.removeCon(con.conID)
}

func connectionID(node string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return node + "-" + strconv.FormatInt(id, 10)
}

// initProxyMetadata initializes just the basic metadata of a proxy. This is decoupled from
// initProxyState such that we can perform authorization before attempting expensive computations to
// fully initialize the proxy.
func (s *DiscoveryService) initProxyMetadata(node *core.Node) (*model.Proxy, error) {
	meta, err := model.ParseMetadata(node.Metadata)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	proxy, err := model.ParseServiceNodeWithMetadata(node.Id, meta)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	// Update the config namespace associated with this proxy
	proxy.ConfigNamespace = model.GetProxyConfigNamespace(proxy)
	proxy.XdsNode = node
	return proxy, nil
}

func (s *DiscoveryService) addCon(conID string, con *Connection) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()
	s.AdsClients[conID] = con
}

func (s *DiscoveryService) removeCon(conID string) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()

	if _, exist := s.AdsClients[conID]; !exist {
		log.Errorf("ADS: Removing connection for non-existing node:%v.", conID)
	} else {
		delete(s.AdsClients, conID)
	}
}

// Compute and send the new configuration for a connection.
func (s *DiscoveryService) pushConnection(con *Connection) error {
	res, err := s.generateData(con)
	if err != nil {
		log.Errorf("generating data returned with error: %s", err)
		return err
	}

	if res == nil {
		return nil
	}

	log.Infof("sending response to proxy: %v", res)
	resp := &discovery.DiscoveryResponse{
		ControlPlane: s.ControlPlane,
		TypeUrl:      resource.AnyType,
		VersionInfo:  "1",
		Nonce:        nonce(),
		Resources:    dp_model.EnvoyFiltersToAny(res),
	}

	if err := con.send(resp); err != nil {
		log.Errorf("error while sending resp to %s", con.peerAddr)
		return err
	}

	return nil
}

func (s *DiscoveryService) generateData(con *Connection) ([]*v1alpha3.EnvoyFilter, error) {
	var efs []*v1alpha3.EnvoyFilter

	if len(s.CacheMap) == 0 {
		return efs, nil
	}

	label := con.proxy.XdsNode.Cluster

	for name, filter := range s.CacheMap {
		if !containLabel(filter, label) {
			continue
		}
		log.Infof("filter: %s would be applied for %v", name, con.peerAddr)

		efs = append(efs, filter)
	}

	return efs, nil
}

func (s *DiscoveryService) updatesHandler() {
	for {
		select {
		case efw := <-s.updatesChan:
			for _, patch := range efw.Patches {
				s.CacheMap[efw.Name+patch.Name+patch.Namespace] = patch.EnvoyFilter
			}

			for _, connection := range s.AdsClients {
				connection.PushChannel <- struct{}{}
			}
		}
	}
}

func containLabel(filter *v1alpha3.EnvoyFilter, label string) bool {
	if filter == nil || filter.WorkloadSelector == nil {
		return false
	}
	labels := strings.Split(filter.WorkloadSelector.Labels[SDP_LABEL], ",")
	return slices.Contains(labels, label)
}
