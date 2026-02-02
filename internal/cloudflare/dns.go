// Package cloudflare provides a wrapper around cloudflare-go for cfgate's needs.
package cloudflare

import (
	"context"
	"fmt"
	"strings"
)

// DNSService handles DNS-specific operations.
// It wraps the cloudflare-go client with cfgate-specific logic.
type DNSService struct {
	// client is the underlying Cloudflare client.
	client Client
}

// NewDNSService creates a new DNSService.
func NewDNSService(client Client) *DNSService {
	return &DNSService{
		client: client,
	}
}

// SyncRecord ensures a DNS record exists with the desired configuration.
// Creates the record if it doesn't exist, updates it if it differs.
// Respects ownership - will NOT update records not owned by cfgate.
// Returns the record, whether it was modified, and any error.
func (s *DNSService) SyncRecord(ctx context.Context, zoneID string, desired DNSRecord) (*DNSRecord, bool, error) {
	// Find existing record
	existing, err := s.FindRecordByName(ctx, zoneID, desired.Name, desired.Type)
	if err != nil {
		return nil, false, fmt.Errorf("failed to find existing record: %w", err)
	}

	// Create if doesn't exist
	if existing == nil {
		record, err := s.client.CreateDNSRecord(ctx, zoneID, desired)
		if err != nil {
			return nil, false, fmt.Errorf("failed to create DNS record: %w", err)
		}
		return record, true, nil
	}

	// Check ownership before updating - only update records we own
	if !IsOwnedByCfgate(existing, "", "") {
		// Record exists but is not owned by cfgate - don't modify
		return existing, false, nil
	}

	// Check if update needed
	if recordsMatch(existing, &desired) {
		return existing, false, nil
	}

	// Update existing record (we own it)
	record, err := s.client.UpdateDNSRecord(ctx, zoneID, existing.ID, desired)
	if err != nil {
		return nil, false, fmt.Errorf("failed to update DNS record: %w", err)
	}

	return record, true, nil
}

// recordsMatch checks if two records have the same content.
func recordsMatch(a, b *DNSRecord) bool {
	return a.Content == b.Content &&
		a.Proxied == b.Proxied &&
		a.TTL == b.TTL &&
		a.Comment == b.Comment
}

// DeleteRecord deletes a DNS record by ID.
func (s *DNSService) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return s.client.DeleteDNSRecord(ctx, zoneID, recordID)
}

// FindRecordByName finds a DNS record by name and type.
// Returns nil if not found.
func (s *DNSService) FindRecordByName(ctx context.Context, zoneID, name, recordType string) (*DNSRecord, error) {
	records, err := s.client.ListDNSRecords(ctx, zoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}

	for _, record := range records {
		if record.Name == name && record.Type == recordType {
			recordCopy := record
			return &recordCopy, nil
		}
	}

	return nil, nil
}

// ListManagedRecords lists all DNS records managed by cfgate.
// Uses ownership markers (TXT records or comments) to identify managed records.
func (s *DNSService) ListManagedRecords(ctx context.Context, zoneID, ownershipPrefix string) ([]DNSRecord, error) {
	records, err := s.client.ListDNSRecords(ctx, zoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}

	var managed []DNSRecord
	for _, record := range records {
		// Check for ownership via comment
		if strings.Contains(record.Comment, "managed by cfgate") {
			managed = append(managed, record)
			continue
		}

		// Check for TXT ownership record
		if record.Type == "TXT" && strings.HasPrefix(record.Name, ownershipPrefix+".") {
			managed = append(managed, record)
		}
	}

	return managed, nil
}

// CreateOwnershipRecord creates or updates a TXT record for ownership tracking.
// Uses upsert pattern: checks if record exists before creating to avoid duplicate errors.
func (s *DNSService) CreateOwnershipRecord(ctx context.Context, zoneID, hostname, tunnelName string, prefix string) error {
	record := BuildOwnershipTXTRecord(hostname, tunnelName, prefix)

	// Check if ownership record already exists
	existing, err := s.FindRecordByName(ctx, zoneID, record.Name, record.Type)
	if err != nil {
		return fmt.Errorf("failed to check existing ownership record: %w", err)
	}

	if existing != nil {
		// Record exists - check if update needed
		if existing.Content == record.Content && existing.Comment == record.Comment {
			return nil // Already up to date
		}
		// Update existing record
		_, err := s.client.UpdateDNSRecord(ctx, zoneID, existing.ID, record)
		if err != nil {
			return fmt.Errorf("failed to update ownership record: %w", err)
		}
		return nil
	}

	// Create new record
	_, err = s.client.CreateDNSRecord(ctx, zoneID, record)
	if err != nil {
		return fmt.Errorf("failed to create ownership record: %w", err)
	}

	return nil
}

// DeleteOwnershipRecord deletes the TXT record for ownership tracking.
func (s *DNSService) DeleteOwnershipRecord(ctx context.Context, zoneID, hostname, prefix string) error {
	txtName := fmt.Sprintf("%s.%s", prefix, hostname)
	record, err := s.FindRecordByName(ctx, zoneID, txtName, "TXT")
	if err != nil {
		return fmt.Errorf("failed to find ownership record: %w", err)
	}

	if record == nil {
		return nil // Already deleted
	}

	return s.DeleteRecord(ctx, zoneID, record.ID)
}

// ResolveZone resolves a zone name to a Zone.
// Returns nil if the zone doesn't exist or isn't accessible.
func (s *DNSService) ResolveZone(ctx context.Context, zoneName string) (*Zone, error) {
	return s.client.GetZoneByName(ctx, zoneName)
}

// ExtractZoneFromHostname extracts the zone name from a hostname.
// For example, "app.example.com" -> "example.com".
func ExtractZoneFromHostname(hostname string) string {
	parts := strings.Split(hostname, ".")
	if len(parts) < 2 {
		return hostname
	}

	// Return last two parts (domain.tld)
	// This is a simple heuristic that works for most cases
	// For complex TLDs like .co.uk, this would need enhancement
	return strings.Join(parts[len(parts)-2:], ".")
}

// BuildCNAMERecord builds a CNAME record for a tunnel.
func BuildCNAMERecord(hostname, tunnelDomain string, proxied bool, comment string) DNSRecord {
	return DNSRecord{
		Type:    "CNAME",
		Name:    hostname,
		Content: tunnelDomain,
		TTL:     1, // Auto TTL
		Proxied: proxied,
		Comment: comment,
	}
}

// BuildOwnershipTXTRecord builds a TXT record for ownership tracking.
func BuildOwnershipTXTRecord(hostname, tunnelName, prefix string) DNSRecord {
	return DNSRecord{
		Type:    "TXT",
		Name:    fmt.Sprintf("%s.%s", prefix, hostname),
		Content: fmt.Sprintf("managed by cfgate, tunnel=%s", tunnelName),
		TTL:     1, // Auto TTL
		Proxied: false,
		Comment: "cfgate ownership record",
	}
}

// IsOwnedByCfgate checks if a DNS record is managed by cfgate.
func IsOwnedByCfgate(record *DNSRecord, ownershipPrefix, tunnelName string) bool {
	// Check comment
	if strings.Contains(record.Comment, "managed by cfgate") {
		if tunnelName == "" {
			return true
		}
		return strings.Contains(record.Comment, fmt.Sprintf("tunnel=%s", tunnelName))
	}

	return false
}
