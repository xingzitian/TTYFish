package engine

// Decide 是"机器人 / AI 玩家"的统一入口：只吃一个 Observation，吐一个动作。
//
// 这是整个项目给 AI 留的接缝——以后接 LLM 只需要另写一个同样签名的函数，
// 而且它天然拿不到别人的手牌（Observation 里根本没有），所以不可能作弊。
func Decide(o Observation) Action {
	if !o.MyTurn || o.Phase != Playing || len(o.Legal) == 0 {
		return nil
	}

	// 有 +4 待质疑：机器人一律认罚（不做心理博弈）。
	if o.Challenge != nil {
		for _, a := range o.Legal {
			if rc, ok := a.(ResolveChallenge); ok && !rc.Challenge {
				return rc
			}
		}
		return nil
	}

	// 注意：机器人**不抓**别人漏喊 UNO。
	// 漏喊的判定和 3 秒宽限由房间负责（引擎不碰时间），如果机器人在这里
	// 抢先抓，人就永远没有反应时间——那就不是"抢喊"，是"必挨罚"。

	nextHand := 99
	nextSeat := (o.Seat + o.Dir + o.SeatCount) % o.SeatCount
	if nextSeat != o.Seat {
		nextHand = o.HandCounts[nextSeat]
	}

	bestID, bestScore := -1, -1
	var draw, pass Action
	seen := make(map[int]bool, len(o.Legal))

	for _, a := range o.Legal {
		switch v := a.(type) {
		case PlayCard:
			// 变色牌会按颜色展开成四个动作，这里按牌去重。
			if seen[v.CardID] {
				continue
			}
			seen[v.CardID] = true
			c, ok := cardByID(o.Hand, v.CardID)
			if !ok {
				continue
			}
			if sc := botScore(c, nextHand, o.SeatCount); sc > bestScore {
				bestScore, bestID = sc, v.CardID
			}
		case DrawCard:
			draw = v
		case Pass:
			pass = v
		}
	}

	if bestID >= 0 {
		c, _ := cardByID(o.Hand, bestID)
		color := c.Color
		if c.Kind == Wild || c.Kind == WildDrawFour {
			color = bestColorOf(o.Hand)
		}
		return PlayCard{
			Seat:   o.Seat,
			CardID: bestID,
			Color:  color,
			// 打完只剩一张就顺势喊 UNO——机器人不会忘记，这是牌局能打完的前提。
			Uno: len(o.Hand) == 2,
		}
	}
	if draw != nil {
		return draw
	}
	return pass
}

// BotMove 保持旧的调用方式，内部转成 Observation 再决策。
func BotMove(s *State, seat int) Action {
	return Decide(Observe(s, seat))
}

func cardByID(hand []Card, id int) (Card, bool) {
	for _, c := range hand {
		if c.ID == id {
			return c, true
		}
	}
	return Card{}, false
}

func bestColorOf(hand []Card) Color {
	var cnt [4]int
	for _, c := range hand {
		if c.Color.Valid() {
			cnt[c.Color]++
		}
	}
	best := Red
	for c := Red; c <= Blue; c++ {
		if cnt[c] > cnt[best] {
			best = c
		}
	}
	return best
}

func botScore(c Card, nextHand, seats int) int {
	switch c.Kind {
	case DrawTwo:
		if nextHand <= 2 {
			return 110
		}
		return 70
	case Skip:
		if nextHand <= 2 {
			return 95
		}
		return 60
	case WildDrawFour:
		if nextHand <= 2 {
			return 105
		}
		return 5
	case Wild:
		return 12
	case Reverse:
		if seats == 2 {
			return 90 // 两人局反转 = 再打一次
		}
		return 30
	default:
		// 手牌越"整"越灵活，所以优先甩掉大数字。
		return 20 + c.Num
	}
}
