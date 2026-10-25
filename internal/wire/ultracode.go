package wire

// Ultracode was once a level of Effort; it is a mode of its own now (Model.Ultracode). A record saved before, or an
// ask from a page of before, that says effort "ultracode" is read as what Claude Code ran for it: `--effort ultracode`
// and `/effort ultracode` were xhigh with ultracode on («Ultracode on … Effort stays xhigh»).

// LegacyUltracode reads an effort of before: "ultracode" is xhigh with ultracode on; any other is itself, mode off.
func LegacyUltracode(e *Effort) (*Effort, bool) {
	if e == nil || *e != EffortUltracode {
		return e, false
	}
	x := EffortXHigh
	return &x, true
}

func (m *Model) UnmarshalJSON(b []byte) error {
	type plain Model
	var v plain
	if err := decodeStrict(b, &v); err != nil {
		return err
	}
	e, ultra := LegacyUltracode(v.Effort)
	v.Effort, v.Ultracode = e, v.Ultracode || ultra
	*m = Model(v)
	return nil
}

func (p *PendingModel) UnmarshalJSON(b []byte) error {
	type plain PendingModel
	var v plain
	if err := decodeStrict(b, &v); err != nil {
		return err
	}
	e, ultra := LegacyUltracode(v.Effort)
	v.Effort, v.Ultracode = e, v.Ultracode || ultra
	*p = PendingModel(v)
	return nil
}

func (c *ModelCmd) UnmarshalJSON(b []byte) error {
	type plain ModelCmd
	var v plain
	if err := decodeStrict(b, &v); err != nil {
		return err
	}
	e, ultra := LegacyUltracode(v.Effort)
	v.Effort = e
	if ultra {
		on := true
		v.Ultracode = &on
	}
	*c = ModelCmd(v)
	return nil
}

// UltracodeAfter is the mode the conversation runs once the ask is taken: the ask's, or, when it does not say, the
// one cur runs.
func (c ModelCmd) UltracodeAfter(cur *Model) bool {
	if c.Ultracode != nil {
		return *c.Ultracode
	}
	return cur != nil && cur.Ultracode
}
