package http

import (
	efwrapperv1 "diploma-prototype/api/efwrapper/v1"
	"diploma-prototype/internal/settings"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	"istio.io/pkg/log"
	"net/http"
)

type Server struct {
	serveMux    *http.ServeMux
	updatesChan chan *efwrapperv1.Configuration
	httpPort    int
}

func NewServer(updatesChan chan *efwrapperv1.Configuration, s *settings.Settings) *Server {
	server := &Server{
		updatesChan: updatesChan,
		httpPort:    s.HttpPort,
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/add/config", server.handler)
	server.serveMux = serveMux

	return server
}

func (s *Server) Run() {
	if err := http.ListenAndServe(fmt.Sprintf(":%v", s.httpPort), s.serveMux); err != nil {
		log.Fatalf("failed to serve http on port %v", s.httpPort)
	}
}

func (s *Server) handler(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		log.Error(err)
		http.Error(writer, "Error while read body content", http.StatusInternalServerError)
		return
	}

	efw := &efwrapperv1.Configuration{}
	err = protojson.Unmarshal(body, efw)

	if err != nil {
		log.Error(err)
		http.Error(writer, "Error while deserialization body", http.StatusInternalServerError)
		return
	}

	s.updatesChan <- efw
}
