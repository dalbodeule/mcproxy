package geo

import (
	"net"

	"github.com/oschwald/geoip2-golang"
)

type Service struct {
	db *geoip2.Reader
}

func Open(path string) (*Service, error) {
	db, err := geoip2.Open(path)
	if err != nil {
		return nil, err
	}
	return &Service{db: db}, nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// CountryCode returns ISO country code (e.g., "KR", "US"). Empty string if not found.
func (s *Service) CountryCode(ip net.IP) string {
	if s == nil || s.db == nil {
		return ""
	}
	rec, err := s.db.City(ip)
	if err != nil || rec == nil || rec.Country.IsoCode == "" {
		return ""
	}
	return rec.Country.IsoCode
}
