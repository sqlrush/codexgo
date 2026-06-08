// Package dbaa reads database connection definitions from an opendb-style
// config (~/.dbaa/config.yaml) so the plugin can connect to a named target
// without the user re-entering host/port/user/password. It is read-only and
// touches only the `connections` list; everything else in the file is ignored.
package dbaa

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Connection mirrors one entry under `connections:` in ~/.dbaa/config.yaml.
type Connection struct {
	Name     string `yaml:"name"`
	DBType   string `yaml:"db_type"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Service  string `yaml:"service"`
	User     string `yaml:"user"`
	SSLMode  string `yaml:"sslmode"`
	Cred     struct {
		Value string `yaml:"value"`
	} `yaml:"credential"`
}

type configFile struct {
	Connections []Connection `yaml:"connections"`
}

// gaussTypes are the db_type values this plugin can drive (it reuses openGauss
// SQL, and GaussDB/openGauss share the wire protocol + driver).
var gaussTypes = map[string]bool{"gaussdb": true, "opengauss": true}

// ConfigPath resolves the opendb config path: $DBAA_CONFIG, else
// $DBAA_HOME/config.yaml, else ~/.dbaa/config.yaml.
func ConfigPath() string {
	if p := os.Getenv("DBAA_CONFIG"); p != "" {
		return p
	}
	home := os.Getenv("DBAA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".dbaa")
		}
	}
	return filepath.Join(home, "config.yaml")
}

// Load reads and parses the connections from the opendb config.
func Load() ([]Connection, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg configFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg.Connections, nil
}

// GaussConnections returns only the GaussDB/openGauss connections, in file order.
func GaussConnections() ([]Connection, error) {
	all, err := Load()
	if err != nil {
		return nil, err
	}
	var out []Connection
	for _, c := range all {
		if gaussTypes[strings.ToLower(c.DBType)] {
			out = append(out, c)
		}
	}
	return out, nil
}

// Resolve finds a GaussDB/openGauss connection by name, or the default (first,
// preferring db_type gaussdb) when name is empty. It returns the connection
// target and a display label.
func Resolve(name string) (db.Target, string, error) {
	conns, err := GaussConnections()
	if err != nil {
		return db.Target{}, "", err
	}
	if len(conns) == 0 {
		return db.Target{}, "", fmt.Errorf("no gaussdb/opengauss connection found in %s", ConfigPath())
	}
	var chosen *Connection
	if name != "" {
		for i := range conns {
			if strings.EqualFold(conns[i].Name, name) {
				chosen = &conns[i]
				break
			}
		}
		if chosen == nil {
			return db.Target{}, "", fmt.Errorf("connection %q not found in %s", name, ConfigPath())
		}
	} else {
		// Default: prefer a db_type=gaussdb entry, else the first gauss/og entry.
		chosen = &conns[0]
		for i := range conns {
			if strings.EqualFold(conns[i].DBType, "gaussdb") {
				chosen = &conns[i]
				break
			}
		}
	}

	database := chosen.Database
	if database == "" {
		database = chosen.Service // oracle-style; harmless fallback
	}
	target := db.Target{
		Host:     chosen.Host,
		Port:     chosen.Port,
		User:     chosen.User,
		Password: chosen.Cred.Value,
		Database: database,
		SSLMode:  chosen.SSLMode,
	}
	label := fmt.Sprintf("%s (%s:%d/%s)", chosen.Name, chosen.Host, chosen.Port, database)
	return target, label, nil
}
