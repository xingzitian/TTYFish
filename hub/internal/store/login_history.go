package store

import (
	"database/sql"
	"sort"
	"time"
)

const loginHistoryLimit = 1000
const loginHistoryRetention = 30 * 24 * time.Hour

// recordLogin runs in the same transaction as the new Web session or SSH lease.
func recordLogin(tx *sql.Tx, accountID, tokenHash, channel, lease string, now time.Time) error {
	result, err := tx.Exec(`INSERT INTO login_history(account_id,channel,auth_method,source_ip,credential_fp,key_name,device_id,user_agent,started_at,lease_id)
 SELECT s.account_id,?,s.auth_method,s.source_ip,s.credential_fp,COALESCE((SELECT name FROM identities WHERE account_id=s.account_id AND fingerprint=s.credential_fp),''),s.device_id,s.user_agent,?,?
 FROM sessions s WHERE s.account_id=? AND s.token_hash=?`, channel, now.UnixMilli(), lease, accountID, tokenHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSessionInvalid
	}
	_, err = tx.Exec(`DELETE FROM login_history WHERE account_id=? AND id NOT IN (SELECT id FROM login_history WHERE account_id=? ORDER BY started_at DESC,id DESC LIMIT ?)`, accountID, accountID, loginHistoryLimit)
	return err
}

type LoginEvent struct {
	ID          int64      `json:"id"`
	AuthMethod  string     `json:"auth_method"`
	SourceIP    string     `json:"source_ip"`
	Fingerprint string     `json:"credential_fingerprint"`
	KeyName     string     `json:"key_name"`
	UserAgent   string     `json:"user_agent"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	EndReason   string     `json:"end_reason"`
	deviceID    string
}

type LoginGroup struct {
	AuthMethod  string       `json:"auth_method"`
	SourceIP    string       `json:"source_ip"`
	Fingerprint string       `json:"credential_fingerprint"`
	KeyName     string       `json:"key_name"`
	UserAgent   string       `json:"user_agent"`
	LatestAt    time.Time    `json:"latest_at"`
	Count       int          `json:"count"`
	Events      []LoginEvent `json:"events"`
}

// LoginHistory groups the complete retained account history before pagination.
func (s *Store) LoginHistory(accountID, channel string, offset, limit int) ([]LoginGroup, int, error) {
	now := time.Now()
	rows, err := s.db.Query(`SELECT h.id,h.auth_method,h.source_ip,h.credential_fp,h.key_name,h.device_id,h.user_agent,h.started_at,
 CASE WHEN h.ended_at IS NULL AND h.channel='ssh' AND t.expires_at<=? THEN t.last_seen_at ELSE h.ended_at END,
 CASE WHEN h.ended_at IS NULL AND h.channel='ssh' AND t.expires_at<=? THEN 'lost' ELSE h.end_reason END
 FROM login_history h LEFT JOIN terminal_sessions t ON t.lease_id=h.lease_id AND h.channel='ssh'
 WHERE h.account_id=? AND h.channel=? AND h.started_at>=? ORDER BY h.started_at DESC,h.id DESC`, now.UnixMilli(), now.UnixMilli(), accountID, channel, now.Add(-loginHistoryRetention).UnixMilli())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	type groupKey struct{ method, ip, fp, device string }
	groups := make([]LoginGroup, 0)
	indices := make(map[groupKey]int)
	for rows.Next() {
		var e LoginEvent
		var start int64
		var end sql.NullInt64
		if err := rows.Scan(&e.ID, &e.AuthMethod, &e.SourceIP, &e.Fingerprint, &e.KeyName, &e.deviceID, &e.UserAgent, &start, &end, &e.EndReason); err != nil {
			return nil, 0, err
		}
		e.StartedAt = time.UnixMilli(start)
		if end.Valid {
			v := time.UnixMilli(end.Int64)
			e.EndedAt = &v
		}
		k := groupKey{method: e.AuthMethod, ip: e.SourceIP, fp: e.Fingerprint}
		if channel == "web" {
			k = groupKey{device: e.deviceID}
			if e.deviceID == "" {
				k = groupKey{method: e.AuthMethod, ip: e.SourceIP, fp: e.Fingerprint}
			}
		}
		i, exists := indices[k]
		if !exists {
			i = len(groups)
			indices[k] = i
			groups = append(groups, LoginGroup{AuthMethod: e.AuthMethod, SourceIP: e.SourceIP, Fingerprint: e.Fingerprint, KeyName: e.KeyName, UserAgent: e.UserAgent, LatestAt: e.StartedAt, Events: []LoginEvent{}})
		}
		groups[i].Events = append(groups[i].Events, e)
		groups[i].Count++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].LatestAt.After(groups[j].LatestAt) })
	total := len(groups)
	if offset >= total {
		return []LoginGroup{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return groups[offset:end], total, nil
}
