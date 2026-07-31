package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

type PackageEvent struct {
	ID           int64     `json:"id"`
	Time         time.Time `json:"time"`
	PackageID    string    `json:"package_id"`
	Manager      string    `json:"manager"`
	Name         string    `json:"name"`
	Architecture string    `json:"architecture"`
	EventType    string    `json:"event_type"`
	OldVersion   string    `json:"old_version,omitempty"`
	NewVersion   string    `json:"new_version,omitempty"`
}

func (s *Store) SavePackageScan(scan model.PackageScan) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingCount int
	if err := tx.QueryRow("SELECT COUNT(*) FROM package_inventory").Scan(&existingCount); err != nil {
		return err
	}
	now := scan.InventoryScannedAt
	if now.IsZero() {
		now = time.Now()
	}
	existing := map[string]model.Package{}
	rows, err := tx.Query(`SELECT id,manager,name,architecture,installed_version,candidate_version,
	  update_state,source,summary,installed_size_bytes,first_seen,last_changed FROM package_inventory`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item model.Package
		var first, changed int64
		if err := rows.Scan(&item.ID, &item.Manager, &item.Name, &item.Architecture,
			&item.InstalledVersion, &item.CandidateVersion, &item.UpdateState, &item.Source,
			&item.Summary, &item.InstalledSizeBytes, &first, &changed); err != nil {
			rows.Close()
			return err
		}
		item.FirstSeen, item.LastChanged = time.Unix(first, 0), time.Unix(changed, 0)
		existing[item.ID] = item
	}
	if err := rows.Close(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(scan.Items))
	for _, item := range scan.Items {
		seen[item.ID] = true
		old, found := existing[item.ID]
		if found && item.UpdateState == "unknown" && old.UpdateState != "unknown" {
			item.UpdateState = old.UpdateState
			item.CandidateVersion = old.CandidateVersion
		}
		firstSeen, changed := now, now
		if found {
			firstSeen, changed = old.FirstSeen, old.LastChanged
			if old.InstalledVersion != item.InstalledVersion {
				changed = now
				if err := insertPackageEvent(tx, now, item, "version_changed", old.InstalledVersion, item.InstalledVersion); err != nil {
					return err
				}
			}
		} else if existingCount > 0 {
			if err := insertPackageEvent(tx, now, item, "installed", "", item.InstalledVersion); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`INSERT INTO package_inventory
		  (id,manager,name,architecture,installed_version,candidate_version,update_state,source,
		   summary,installed_size_bytes,first_seen,last_changed,last_seen)
		  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		  ON CONFLICT(id) DO UPDATE SET installed_version=excluded.installed_version,
		    candidate_version=excluded.candidate_version,update_state=excluded.update_state,
		    source=excluded.source,summary=excluded.summary,
		    installed_size_bytes=excluded.installed_size_bytes,
		    last_changed=excluded.last_changed,last_seen=excluded.last_seen`,
			item.ID, item.Manager, item.Name, item.Architecture, item.InstalledVersion,
			item.CandidateVersion, item.UpdateState, item.Source, item.Summary,
			item.InstalledSizeBytes, firstSeen.Unix(), changed.Unix(), now.Unix())
		if err != nil {
			return err
		}
	}
	for id, item := range existing {
		if seen[id] {
			continue
		}
		if existingCount > 0 {
			if err := insertPackageEvent(tx, now, item, "removed", item.InstalledVersion, ""); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("DELETE FROM package_inventory WHERE id=?", id); err != nil {
			return err
		}
	}
	metadata := scan.MetadataRefreshedAt.Unix()
	if scan.MetadataRefreshedAt.IsZero() {
		if err := tx.QueryRow("SELECT metadata_refreshed_at FROM package_scan_state WHERE singleton=1").Scan(&metadata); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE package_scan_state SET hostname=?,manager=?,inventory_available=?,
	  update_check_available=?,inventory_scanned_at=?,metadata_refreshed_at=?,error=? WHERE singleton=1`,
		scan.Hostname, scan.Manager, scan.InventoryAvailable, scan.UpdateCheckAvailable,
		now.Unix(), metadata, scan.Error)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func insertPackageEvent(tx *sql.Tx, at time.Time, item model.Package, eventType, oldVersion, newVersion string) error {
	_, err := tx.Exec(`INSERT INTO package_events
	  (ts,package_id,manager,name,architecture,event_type,old_version,new_version)
	  VALUES(?,?,?,?,?,?,?,?)`, at.Unix(), item.ID, item.Manager, item.Name,
		item.Architecture, eventType, oldVersion, newVersion)
	return err
}

func (s *Store) PackageScanState() (model.PackageScan, error) {
	var result model.PackageScan
	var inventory, updates bool
	var scanned, metadata int64
	err := s.db.QueryRow(`SELECT hostname,manager,inventory_available,update_check_available,
	  inventory_scanned_at,metadata_refreshed_at,error FROM package_scan_state WHERE singleton=1`).
		Scan(&result.Hostname, &result.Manager, &inventory, &updates, &scanned, &metadata, &result.Error)
	result.InventoryAvailable, result.UpdateCheckAvailable = inventory, updates
	if scanned > 0 {
		result.InventoryScannedAt = time.Unix(scanned, 0)
	}
	if metadata > 0 {
		result.MetadataRefreshedAt = time.Unix(metadata, 0)
	}
	return result, err
}

func (s *Store) SetPackageScanError(message string) error {
	_, err := s.db.Exec("UPDATE package_scan_state SET error=? WHERE singleton=1", message)
	return err
}

func (s *Store) Packages(query, status, manager, sortBy, order string, page, perPage int) ([]model.Package, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 500 {
		perPage = 100
	}
	column := map[string]string{"name": "name", "status": "update_state", "size": "installed_size_bytes", "changed": "last_changed"}[sortBy]
	if column == "" {
		column = "name"
	}
	direction := "ASC"
	if strings.EqualFold(order, "desc") {
		direction = "DESC"
	}
	where := []string{"(?='' OR lower(name) LIKE ? OR lower(summary) LIKE ? OR lower(source) LIKE ?)",
		"(?='' OR update_state=?)", "(?='' OR manager=?)"}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	args := []any{query, pattern, pattern, pattern, status, status, manager, manager}
	var total int
	countSQL := "SELECT COUNT(*) FROM package_inventory WHERE " + strings.Join(where, " AND ")
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	statement := fmt.Sprintf(`SELECT id,manager,name,architecture,installed_version,candidate_version,
	  update_state,source,summary,installed_size_bytes,first_seen,last_changed
	  FROM package_inventory WHERE %s ORDER BY %s %s,name ASC LIMIT ? OFFSET ?`,
		strings.Join(where, " AND "), column, direction)
	rows, err := s.db.Query(statement, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []model.Package
	for rows.Next() {
		var item model.Package
		var first, changed int64
		if err := rows.Scan(&item.ID, &item.Manager, &item.Name, &item.Architecture,
			&item.InstalledVersion, &item.CandidateVersion, &item.UpdateState, &item.Source,
			&item.Summary, &item.InstalledSizeBytes, &first, &changed); err != nil {
			return nil, 0, err
		}
		item.FirstSeen, item.LastChanged = time.Unix(first, 0), time.Unix(changed, 0)
		result = append(result, item)
	}
	return result, total, rows.Err()
}

func (s *Store) Package(id string) (model.Package, error) {
	var item model.Package
	var first, changed int64
	err := s.db.QueryRow(`SELECT id,manager,name,architecture,installed_version,candidate_version,
	  update_state,source,summary,installed_size_bytes,first_seen,last_changed
	  FROM package_inventory WHERE id=?`, id).Scan(&item.ID, &item.Manager, &item.Name,
		&item.Architecture, &item.InstalledVersion, &item.CandidateVersion, &item.UpdateState,
		&item.Source, &item.Summary, &item.InstalledSizeBytes, &first, &changed)
	if err == nil {
		item.FirstSeen, item.LastChanged = time.Unix(first, 0), time.Unix(changed, 0)
	}
	return item, err
}

func (s *Store) PackageCounts() (int, int, int, error) {
	var total, updates, unknown int
	err := s.db.QueryRow(`SELECT COUNT(*),
	  COALESCE(SUM(CASE WHEN update_state='update_available' THEN 1 ELSE 0 END),0),
	  COALESCE(SUM(CASE WHEN update_state='unknown' THEN 1 ELSE 0 END),0)
	  FROM package_inventory`).Scan(&total, &updates, &unknown)
	return total, updates, unknown, err
}

func (s *Store) PackageEvents(from time.Time, query, eventType string, page, perPage int) ([]PackageEvent, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 500 {
		perPage = 100
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	args := []any{from.Unix(), query, pattern, eventType, eventType}
	where := "ts>=? AND (?='' OR lower(name) LIKE ?) AND (?='' OR event_type=?)"
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM package_events WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id,ts,package_id,manager,name,architecture,event_type,
	  old_version,new_version FROM package_events WHERE `+where+`
	  ORDER BY ts DESC LIMIT ? OFFSET ?`, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []PackageEvent
	for rows.Next() {
		var item PackageEvent
		var timestamp int64
		if err := rows.Scan(&item.ID, &timestamp, &item.PackageID, &item.Manager, &item.Name,
			&item.Architecture, &item.EventType, &item.OldVersion, &item.NewVersion); err != nil {
			return nil, 0, err
		}
		item.Time = time.Unix(timestamp, 0)
		result = append(result, item)
	}
	return result, total, rows.Err()
}

func (s *Store) PrunePackageEvents(before time.Time) error {
	_, err := s.db.Exec("DELETE FROM package_events WHERE ts<?", before.Unix())
	return err
}
