package engine

var cardNames = map[Kind]string{
	ExplodingKitten: "爆炸猫",
	Defuse:          "拆弹",
	Attack:          "攻击",
	Skip:            "跳过",
	Reverse:         "反转",
	Favor:           "索要",
	Shuffle:         "洗牌",
	SeeTheFuture:    "预知未来",
	Nope:            "反制",
	TacoCat:         "卷饼猫",
	Cattermelon:     "西瓜猫",
	HairyPotatoCat:  "毛茸茸土豆猫",
	BeardCat:        "胡子猫",
	RainbowCat:      "彩虹猫",
}

// Name is for display only; Kind remains the stable wire identifier.
func (k Kind) Name() string {
	if name, ok := cardNames[k]; ok {
		return name
	}
	return string(k)
}

func ParseKind(name string) (Kind, bool) {
	for kind, label := range cardNames {
		if name == label || name == string(kind) {
			return kind, true
		}
	}
	return "", false
}
