package settings

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type Settings struct {
	GrpcPort int `envconfig:"GRPC_PORT" default:"40003"`
	HttpPort int `envconfig:"HTTP_PORT" default:"40004"`
}

func GetSettings() *Settings {
	var s Settings
	if err := envconfig.Process("", &s); err != nil {
		panic(fmt.Sprintf("failed to load application settings: %v", err))
	}
	return &s
}
