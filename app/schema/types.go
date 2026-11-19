package schema

func s(max int) Field             { return Field{Kind: Str, Max: max} }
func i(lo, hi float64) Field      { return Field{Kind: Int, Ranged: true, Lo: lo, Hi: hi} }
func n(lo, hi float64) Field      { return Field{Kind: Num, Ranged: true, Lo: lo, Hi: hi} }
func b() Field                    { return Field{Kind: Bool} }
func list(max int, e Field) Field { return Field{Kind: Arr, Max: max, Elem: &e} }
func line(max int) Field          { return Field{Kind: Str, Max: max, Line: true} }

func closed(fields map[string]Field, required ...string) Field {
	return Field{Kind: Obj, Fields: fields, Required: required}
}

func open(fields map[string]Field, required ...string) Field {
	return Field{Kind: Obj, Fields: fields, Required: required, Open: true}
}

const (
	big     = 1e12
	maxID   = 1e9
	maxLvl  = 100
	maxRank = 30
)

var (
	level = i(1, maxLvl)
	id    = i(0, maxID)
	count = i(0, big)
	money = i(-big, big)
	guid  = s(60)
	unix  = i(0, 1e11)
	when  = Field{Kind: Int, Ranged: true, Lo: 0, Hi: 1e11, Time: true}
	name  = s(60)
)

var player = closed(map[string]Field{
	"name": name, "guid": guid, "class": s(20), "level": level,
	"guild": s(60), "guildRank": s(40), "race": s(20), "faction": s(10),
	"pvpRank": i(0, maxRank),
}, "name")

var source = open(map[string]Field{"id": id, "kind": s(20), "name": name})

var killer = closed(map[string]Field{
	"kind": s(20), "id": id, "name": name, "spell": s(80), "spellId": id,
})

var common = map[string]Field{
	"id":   s(80),
	"type": s(20),
	"at":   i(0, 1e11),
	"character": open(map[string]Field{
		"name": name, "realm": s(60), "guid": guid, "class": s(20), "level": level,
		"surname": s(60),
		"guild":   s(60), "inGuild": b(),
		"client": open(map[string]Field{"project": i(0, 1000), "build": i(0, 1e7), "faction": s(20),
			"hardcore": b(), "region": i(0, 100), "realmNormalized": s(60)}),
	}, "name"),
	"addonVersion": s(20),
}

func record(isOpen bool, fields map[string]Field, required ...string) Field {
	all := map[string]Field{}
	for k, v := range common {
		all[k] = v
	}
	for k, v := range fields {
		all[k] = v
	}
	return Field{Kind: Obj, Fields: all, Open: isOpen,
		Required: append([]string{"id", "type", "at", "character"}, required...)}
}

func own(fields map[string]Field, required ...string) Field {
	return record(true, fields, required...)
}

func observed(fields map[string]Field, required ...string) Field {
	return record(false, fields, required...)
}

var Types = map[string]Field{
	"level": own(map[string]Field{"level": level, "zone": s(60), "subzone": s(60), "prevLevelSeconds": count}, "level"),
	"xp": own(map[string]Field{
		"level": level, "xp": count, "xpMax": count, "rested": count, "why": s(20),
	}),
	"quest": own(map[string]Field{
		"questId": id, "name": s(80), "questLevel": count, "xp": count, "copper": money, "giver": source,
		"zone": s(60), "subzone": s(60),
	}),
	"questsDone": own(map[string]Field{"count": count, "questIds": list(50000, id)}, "questIds"),
	"instance": own(map[string]Field{
		"event": s(20), "kind": s(20), "name": s(80), "instanceId": id, "difficulty": s(40),
		"difficultyId": id, "maxPlayers": i(0, 100), "groupSize": i(0, 100), "durationSeconds": count,
		"encounter": s(80), "encounterId": id,
		"visitId": s(40), "enterAt": unix, "leaveAt": unix, "seconds": count, "reason": s(20),
		"gearAtEnter": list(19, closed(map[string]Field{"slot": i(1, 19), "itemId": id,
			"name": s(80), "icon": i(0, 1e9),
			"context": i(0, 1e6)}, "slot", "itemId")),
	}),
	"session": own(map[string]Field{
		"loginAt": unix, "logoutAt": unix, "lastSeenAt": unix, "seconds": count, "end": s(10),
		"reloads": i(0, 10000),
		"client": closed(map[string]Field{
			"version": s(20), "build": s(20), "interface": i(0, 1e7), "projectId": i(0, 1000),
			"locale": s(10), "maxLevel": level,
		}),
	}, "loginAt"),
	"kills": own(map[string]Field{
		"sessionLoginAt": unix, "kills": count, "xp": count, "restedXp": count, "groupKills": count,
		"instanceKills": count, "instanceXp": count, "secretLines": count, "questXp": count,
		"firstAt": when, "lastAt": when, "otherKills": count,
		"creatures": list(20, closed(map[string]Field{"name": line(60), "kills": count, "xp": count}, "name", "kills")),
	}, "sessionLoginAt"),
	"gear": own(map[string]Field{
		"slots": list(19, closed(map[string]Field{"slot": i(1, 19), "itemId": id, "quality": i(0, 10),
			"name": s(80), "icon": i(0, 1e9),
			"context": i(0, 1e6),
			"setId":   id, "setName": s(80), "setPieces": i(1, 50)}, "slot", "itemId")),
	}, "slots"),
	"groupSession": own(map[string]Field{
		"sessionId": s(40), "start": unix, "end": unix, "kind": s(10), "maxSize": i(0, 40),
		"members": list(80, closed(map[string]Field{
			"name": name, "class": s(20), "level": level, "race": s(20), "guildName": s(60),
			"joinedAt": unix, "leftAt": unix, "self": b(),
		}, "name")),
		"visits": list(200, s(40)),
	}, "start", "members"),
	"fight": observed(map[string]Field{
		"encounterId": id, "encounterName": s(80), "instanceId": id, "visitId": s(40),
		"difficultyId": id, "difficulty": s(40), "start": unix, "seconds": i(0, 86400),
		"outcome": s(10), "outcomeSource": s(20), "attempt": i(1, 10000), "groupSize": i(0, 100),
		"selfInInstance": b(), "selfAlive": b(),
		"aliveAtEnd": list(40, s(64)), "deadAtEnd": list(40, s(64)),
		"roles":    closed(map[string]Field{"tank": i(0, 100), "healer": i(0, 100), "damager": i(0, 100), "none": i(0, 100)}),
		"selfRole": s(10), "bossPct": i(0, 100),
	}, "encounterId", "start", "seconds", "outcome"),
	"lootHistory": observed(map[string]Field{
		"dropKey": s(120), "encounterId": id, "visitId": s(40), "itemId": id, "quality": i(0, 10),
		"rolls":  list(40, closed(map[string]Field{"name": s(64), "choice": s(20), "value": i(0, 1000)}, "name", "choice")),
		"winner": s(64),
	}, "dropKey", "itemId"),
	"loot": own(map[string]Field{
		"itemId": id, "name": s(80), "quality": i(0, 10), "count": i(0, 10000), "how": s(20), "context": i(0, 1e6),
		"source": source, "zone": s(60), "subzone": s(60),
	}),
	"sold": own(map[string]Field{
		"itemId": id, "name": s(80), "quality": i(0, 10), "count": i(0, 10000), "copper": money,
		"merchant": source, "zone": s(60), "subzone": s(60),
	}),
	"gold":   own(map[string]Field{"copper": i(0, big), "delta": money, "context": s(40)}),
	"death":  own(map[string]Field{"level": level, "killer": killer, "zone": s(60), "subzone": s(60)}),
	"played": own(map[string]Field{"totalSeconds": count, "levelSeconds": count}),
	"talents": own(map[string]Field{
		"unspent": i(0, 200),
		"specId":  id, "specName": s(40), "role": s(10),
		"trees": list(10, open(map[string]Field{
			"name": s(40), "points": i(0, 200),
			"talents": list(100, open(map[string]Field{
				"name": s(80), "tier": i(0, 20), "column": i(0, 10), "rank": i(0, 10), "maxRank": i(0, 10),
			})),
		})),
	}, "trees"),
	"professions": own(map[string]Field{
		"professions": list(20, open(map[string]Field{
			"name": s(40), "skill": i(0, 1000), "maxSkill": i(0, 1000),
			"recipes": list(2000, open(map[string]Field{"itemId": id, "name": s(80)})),
		})),
	}, "professions"),
	"reputations": own(map[string]Field{
		"factions": list(500, open(map[string]Field{
			"id": id, "name": s(80), "standingId": i(0, 10),
			"barMin": money, "barMax": money, "barValue": money,
		})),
	}, "factions"),
	"honor": own(map[string]Field{
		"rank": i(0, maxRank), "rankProgress": n(0, 1), "honorLevel": i(0, 1000),
		"today":     open(map[string]Field{"hk": count, "dk": count, "honor": count}),
		"yesterday": open(map[string]Field{"hk": count, "dk": count, "honor": count}),
		"thisWeek":  open(map[string]Field{"hk": count, "honor": count}),
		"lastWeek":  open(map[string]Field{"hk": count, "dk": count, "honor": count, "standing": count}),
		"lifetime":  open(map[string]Field{"hk": count, "dk": count, "highestRank": i(0, maxRank)}),
	}),
	"featsProbe": own(map[string]Field{
		"session": i(0, 1e11),
		"probe":   {Kind: Obj, Open: true},
	}, "session", "probe"),
	"guildChat": own(map[string]Field{
		"day": s(10), "guild": s(60), "sent": i(0, 10000), "seen": i(0, 100000), "unreadable": i(0, 100000),
	}, "day"),

	"rankNames": observed(map[string]Field{
		"guild": s(60),
		"ranks": list(20, closed(map[string]Field{"index": i(0, 20), "name": s(40), "flags": list(40, s(60))})),
	}, "ranks"),
	"guildRoster": observed(map[string]Field{
		"guild": s(60), "complete": b(), "memberCount": i(0, 100000),
		"members": list(100000, closed(map[string]Field{
			"name": name, "guid": guid, "class": s(20), "level": level, "rankIndex": i(0, 20),
			"online": b(), "offlineHours": i(0, 1e6),
			"rankName": s(40), "status": i(0, 2), "mobile": b(),
		}, "name")),
	}, "members"),
	"zones": own(map[string]Field{
		"day": s(10), "count": i(0, 1000), "zones": list(1000, s(60)),
		"subzoneCount": i(0, 5000), "subzones": list(5000, s(130)),
		"perZone": list(1000, closed(map[string]Field{
			"zone": s(60), "seconds": i(0, 1e7), "xp": i(0, 1e9), "kills": i(0, 1e7),
			"quests": i(0, 1e6), "questXp": i(0, 1e9), "hks": i(0, 1e6),
		}, "zone", "seconds")),
	}, "day", "count"),
	"moments":   own(map[string]Field{"kind": s(30)}, "kind"),
	"social":    own(map[string]Field{"kind": s(30), "emote": s(30), "target": s(60)}, "kind"),
	"dayCounts": own(map[string]Field{"day": s(10)}, "day"),
	"lockouts": own(map[string]Field{
		"instances": list(50, open(map[string]Field{"name": s(80), "lockoutId": i(0, 1e10), "difficultyId": id,
			"encounters": i(0, 100), "progress": i(0, 100), "maxPlayers": i(0, 100)})),
	}, "instances"),
	"charter": own(map[string]Field{
		"guild": s(60), "action": s(10), "signatures": i(0, 1000),
		"signers": list(10, s(60)),
	}, "guild", "action"),
	"guildLog": observed(map[string]Field{
		"guild": s(60), "entries": i(0, 100000),
		"events": list(10000, closed(map[string]Field{
			"kind": s(20), "player": name, "by": name, "rank": s(40), "hoursAgo": i(0, 1e6),
		}, "kind")),
	}, "events"),
	"gearChange": own(map[string]Field{"slot": i(1, 19), "oldItemId": id, "newItemId": id, "quality": i(0, 10), "setId": id}, "slot"),
	"itemFacts": own(map[string]Field{
		"itemId": id, "name": s(80), "quality": i(0, 10), "classId": i(0, 1000), "subclassId": i(0, 1000), "equipLoc": s(30),
		"bindType": i(0, 100), "minLevel": i(0, 1000), "sellPrice": money, "stackCount": i(0, 100000), "expansionId": i(0, 1000),
		"setId": id, "icon": i(0, 1e9),
	}, "itemId"),
	"charStats": own(map[string]Field{
		"day": s(10), "level": level, "health": count, "power": count, "powerType": s(20),
		"strength": count, "agility": count, "stamina": count, "intellect": count, "spirit": count, "armor": count,
		"attackPower": count, "spellPower": count,
		"crit": n(0, 1000), "haste": n(0, 1000), "mastery": n(0, 1000), "dodge": n(0, 1000), "parry": n(0, 1000), "block": n(0, 1000),
	}, "day"),
	"currencies": own(map[string]Field{
		"currencies": list(300, open(map[string]Field{"id": id, "quantity": count, "max": count, "weekly": count}, "id", "quantity")),
	}, "currencies"),
	"inventory": own(map[string]Field{
		"bags": list(400, open(map[string]Field{"itemId": id, "count": count}, "itemId", "count")),
		"free": i(0, 1000), "total": i(0, 1000),
		"bank": list(1000, open(map[string]Field{"itemId": id, "count": count}, "itemId", "count")),
	}, "bags"),
	"spellbook": own(map[string]Field{"spellIds": list(3000, id), "count": i(0, 100000)}, "spellIds"),
	"spellUse": own(map[string]Field{
		"day":   s(10),
		"casts": list(300, open(map[string]Field{"spellId": id, "count": count}, "spellId", "count")),
	}, "day", "casts"),
	"collections": own(map[string]Field{
		"mounts":       open(map[string]Field{"count": count, "ids": list(2000, id)}),
		"titles":       open(map[string]Field{"count": count, "ids": list(500, id), "current": count}),
		"achievements": open(map[string]Field{"points": count, "completed": count}),
	}),
	"guildNews": observed(map[string]Field{
		"guild":   s(60),
		"entries": list(200, closed(map[string]Field{"kind": s(20), "player": s(80), "itemId": id, "achievementId": id, "level": level, "hoursAgo": i(0, 1e6)}, "kind")),
	}, "entries"),
	"guildInfo": observed(map[string]Field{
		"guild": s(60), "motdLength": i(0, 100000), "infoLength": i(0, 100000), "motd": s(500), "info": s(500), "changedAt": unix,
	}, "guild"),
	"calendar": observed(map[string]Field{
		"guild": s(60),
		"events": list(2000, closed(map[string]Field{"kind": s(20), "start": s(20), "title": s(80),
			"endsAt": s(20), "creator": s(60), "inviteStatus": i(0, 100), "eventType": i(0, 100), "locked": b(), "holiday": s(80)})),
	}, "events"),

	"bossKill": observed(map[string]Field{
		"kind": s(20), "encounter": s(80), "encounterId": id, "instance": s(80), "instanceId": id,
		"difficulty": s(40), "difficultyId": id, "groupSize": i(0, 100), "group": list(100, player),
	}, "group"),
	"lootWon": observed(map[string]Field{
		"itemId": id, "name": s(80), "quality": i(0, 10), "count": i(0, 10000), "how": s(20),
		"instance": s(80), "instanceId": id, "winner": player,
	}, "winner"),
	"bgMatch": observed(map[string]Field{
		"battleground": s(80), "instanceId": id, "winner": s(20), "durationSeconds": count,
		"players": list(200, closed(map[string]Field{
			"name": name, "class": s(20), "faction": s(10), "rank": i(0, maxRank),
			"killingBlows": count, "honorableKills": count, "deaths": count, "honor": count,
			"stats": list(20, closed(map[string]Field{"id": i(0, 1e6), "value": i(0, 1e9)}, "id", "value")),
		}, "name")),
	}, "players"),
	"inspect": observed(map[string]Field{
		"subject": player,
		"items":   list(40, closed(map[string]Field{"slot": i(0, 40), "itemId": id, "name": s(80), "icon": i(0, 1e9), "context": i(0, 1e6)})),
		"honor": closed(map[string]Field{
			"todayHK":   count,
			"yesterday": closed(map[string]Field{"hk": count, "honor": count}),
			"thisWeek":  closed(map[string]Field{"hk": count, "honor": count}),
			"lastWeek":  closed(map[string]Field{"hk": count, "honor": count, "standing": count}),
			"lifetime":  closed(map[string]Field{"hk": count, "highestRank": i(0, maxRank)}),
		}),
	}, "subject"),
	"duel":     observed(map[string]Field{"how": s(20), "winner": player, "loser": player, "zone": s(60), "subzone": s(60)}, "winner", "loser"),
	"sighting": observed(mergeFields(player.Fields, map[string]Field{"via": s(20)}), "name"),
	"groupDeath": observed(map[string]Field{
		"member": player, "killer": killer, "instance": s(80), "instanceId": id,
	}, "member"),
}

func mergeFields(a, b map[string]Field) map[string]Field {
	out := map[string]Field{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
