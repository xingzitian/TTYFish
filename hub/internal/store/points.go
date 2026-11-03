package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// PTTenths returns the balance in units of 0.1 PT, including zero for old accounts.
func (s *Store) PTTenths(accountID string) (int64, error) {
	var balance int64
	err := s.db.QueryRow(`SELECT COALESCE(p.tenths,0) FROM accounts a
		LEFT JOIN pt_balances p ON p.account_id=a.id WHERE a.id=?`, accountID).Scan(&balance)
	return balance, err
}

// AwardUNO atomically records one final result and adds score tenths of a PT.
func (s *Store) AwardUNO(matchID, accountID string, score int64) error {
	if matchID == "" || accountID == "" || score < 0 {
		return errors.New("invalid UNO PT reward")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previousAccount string
	var previousScore int64
	err = tx.QueryRow(`SELECT account_id,score FROM uno_pt_rewards WHERE match_id=?`, matchID).
		Scan(&previousAccount, &previousScore)
	if err == nil {
		if previousAccount != accountID || previousScore != score {
			return fmt.Errorf("UNO PT result conflict for match %s", matchID)
		}
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO uno_pt_rewards(match_id,account_id,score,created_at) VALUES(?,?,?,?)`,
		matchID, accountID, score, time.Now().UnixMilli()); err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO pt_balances(account_id,tenths) VALUES(?,?)
		ON CONFLICT(account_id) DO UPDATE SET tenths=tenths+excluded.tenths
		WHERE tenths<=?`, accountID, score, int64(math.MaxInt64)-score)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("PT balance overflow")
	}
	return tx.Commit()
}
