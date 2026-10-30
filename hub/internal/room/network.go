package room

// Enter only joins an existing room; unlike legacy Join, typos never create rooms.
func (reg *Registry) Enter(code, fp, name string) (*Room, error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	code = NormalizeCode(code)
	if !ValidCode(code) {
		return nil, ErrCode
	}
	rm, ok := reg.rooms[code]
	if !ok || rm.IsClosed() {
		return nil, ErrNotFound
	}
	if _, err := rm.Join(fp, name); err != nil {
		return nil, err
	}
	return rm, nil
}

func (reg *Registry) Close() {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for code, rm := range reg.rooms {
		rm.Close()
		delete(reg.rooms, code)
	}
}
