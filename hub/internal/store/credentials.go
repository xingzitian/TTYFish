package store

import (
	"errors"
	"fmt"
)

var ErrConflict = errors.New("用户名或公钥已被使用")
var ErrLastCredential = errors.New("不能删除最后一种登录凭证，请先设置密码或添加其他公钥")

func accountWriteError(err error) error {
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&255 == 19 {
		return fmt.Errorf("%w", ErrConflict)
	}
	return err
}

// RemoveIdentity checks ownership and remaining credentials in the same transaction.
func (s *Store) RemoveIdentity(id, fp string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var owns, keys, passwords int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM identities WHERE account_id=? AND fingerprint=?`, id, fp).Scan(&owns); err != nil {
		return false, err
	}
	if owns == 0 {
		return false, nil
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM identities WHERE account_id=?`, id).Scan(&keys); err != nil {
		return false, err
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM passwords WHERE account_id=?`, id).Scan(&passwords); err != nil {
		return false, err
	}
	if keys <= 1 && passwords == 0 {
		return false, ErrLastCredential
	}
	if _, err = tx.Exec(`DELETE FROM identities WHERE account_id=? AND fingerprint=?`, id, fp); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
