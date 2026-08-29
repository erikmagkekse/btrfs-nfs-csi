package models

import (
	"net/url"
	"strconv"
)

// ListOpts configures list endpoint queries (pagination + label filtering).
type ListOpts struct {
	After  string   // opaque cursor from a previous response's Next field
	Limit  int      // items per page (0 or negative = use client default; positive = explicit)
	Labels []string // label filters in "key=value" format
}

// Query builds url.Values for a list request. defaultLimit is used when Limit
// is zero or negative (i.e. the caller did not specify an explicit page size).
func (o ListOpts) Query(defaultLimit int) url.Values {
	q := GenerateLabelQuery(o.Labels)
	if o.After != "" {
		q.Set("after", o.After)
	}
	limit := o.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

// GenerateLabelQuery converts label filters to url.Values with repeated "label" keys.
func GenerateLabelQuery(labels []string) url.Values {
	v := make(url.Values)
	for _, l := range labels {
		v.Add("label", l)
	}
	return v
}
