package validate

// DNSConfig is the top-level struct corresponding to the YAML structure.
type DNSConfig struct {
	InternalDNS []DNSRecord `yaml:"internal_dns"`
	ExternalDNS []DNSRecord `yaml:"external_dns"`
}

// DNSRecord represents each DNS entry with a "name" field.
type DNSRecord struct {
	Name string `yaml:"name"`
}
