package server

import (
	"net/http"
	"sort"

	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/version"
)

func (s *Server) handleMetaVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, version.Info())
}

func (s *Server) handleMetaSources(w http.ResponseWriter, r *http.Request) {
	specs := source.Specs()
	writeList(w, specs, len(specs))
}

func (s *Server) handleMetaDestinations(w http.ResponseWriter, r *http.Request) {
	specs := dest.Specs()
	writeList(w, specs, len(specs))
}

func (s *Server) handleMetaNotifiers(w http.ResponseWriter, r *http.Request) {
	specs := notify.Specs()
	writeList(w, specs, len(specs))
}

func (s *Server) handleMetaTools(w http.ResponseWriter, r *http.Request) {
	items := engine.Tools(r.Context())
	writeList(w, items, len(items))
}

func (s *Server) handleMetaTimezones(w http.ResponseWriter, r *http.Request) {
	zones := timezones()
	writeList(w, zones, len(zones))
}

var commonTimezones = []string{
	"UTC",
	"Africa/Cairo", "Africa/Johannesburg", "Africa/Lagos", "Africa/Nairobi",
	"America/Anchorage", "America/Argentina/Buenos_Aires", "America/Bogota", "America/Chicago",
	"America/Denver", "America/Halifax", "America/Los_Angeles", "America/Mexico_City",
	"America/New_York", "America/Phoenix", "America/Sao_Paulo", "America/Toronto", "America/Vancouver",
	"Asia/Bangkok", "Asia/Dubai", "Asia/Hong_Kong", "Asia/Jakarta", "Asia/Jerusalem",
	"Asia/Kolkata", "Asia/Karachi", "Asia/Manila", "Asia/Seoul", "Asia/Shanghai",
	"Asia/Singapore", "Asia/Taipei", "Asia/Tokyo",
	"Atlantic/Reykjavik",
	"Australia/Adelaide", "Australia/Brisbane", "Australia/Melbourne", "Australia/Perth", "Australia/Sydney",
	"Europe/Amsterdam", "Europe/Athens", "Europe/Belgrade", "Europe/Berlin", "Europe/Brussels",
	"Europe/Bucharest", "Europe/Budapest", "Europe/Copenhagen", "Europe/Dublin", "Europe/Helsinki",
	"Europe/Istanbul", "Europe/Kyiv", "Europe/Lisbon", "Europe/London", "Europe/Madrid",
	"Europe/Moscow", "Europe/Oslo", "Europe/Paris", "Europe/Prague", "Europe/Riga",
	"Europe/Rome", "Europe/Sofia", "Europe/Stockholm", "Europe/Tallinn", "Europe/Vienna",
	"Europe/Vilnius", "Europe/Warsaw", "Europe/Zurich",
	"Pacific/Auckland", "Pacific/Honolulu",
}

func timezones() []string {
	out := make([]string, len(commonTimezones))
	copy(out, commonTimezones)
	sort.Strings(out[1:])
	return out
}
