package geo

import (
	"fmt"
	"net"

	"github.com/oschwald/geoip2-golang"
)

type Service struct {
	db  *geoip2.Reader
	asn *geoip2.Reader
}

func Open(path string) (*Service, error) {
	return OpenWithASN(path, "")
}

func OpenWithASN(cityPath, asnPath string) (*Service, error) {
	service := &Service{}
	var err error
	if cityPath != "" {
		service.db, err = geoip2.Open(cityPath)
		if err != nil {
			return nil, err
		}
	}
	if asnPath != "" {
		service.asn, err = geoip2.Open(asnPath)
		if err != nil {
			_ = service.Close()
			return nil, err
		}
	}
	return service, nil
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	var first error
	if s.db != nil {
		first = s.db.Close()
	}
	if s.asn != nil {
		if err := s.asn.Close(); first == nil {
			first = err
		}
	}
	return first
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

// ASN returns a normalized MaxMind autonomous system identifier such as
// "AS12345". It is empty when an ASN database is not configured or has no
// record for the address.
func (s *Service) ASN(ip net.IP) string {
	if s == nil || s.asn == nil {
		return ""
	}
	rec, err := s.asn.ASN(ip)
	if err != nil || rec == nil || rec.AutonomousSystemNumber == 0 {
		return ""
	}
	return fmt.Sprintf("AS%d", rec.AutonomousSystemNumber)
}
