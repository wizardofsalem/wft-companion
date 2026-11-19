local _, ns = ...

local window
local WINDOW = 10
local levels = {}

local function giver()
    local kind, id = ns.ParseGUID(ns.Call("UnitGUID", "npc"))
    if kind == "unknown" or kind == "player" then return { kind = "unknown" } end
    return { kind = kind, id = id, name = ns.Clean(ns.Call("UnitName", "npc"), 60) }
end

local function readLevel(questId)
    local level = ns.Call("C_QuestLog.GetQuestDifficultyLevel", questId)
    if type(level) == "number" and level > 0 then return math.floor(level) end
    local index = ns.Call("C_QuestLog.GetLogIndexForQuestID", questId)
    if type(index) ~= "number" or index <= 0 then return end
    local info = ns.Call("C_QuestLog.GetInfo", index)
    level = type(info) == "table" and info.level
    if type(level) == "number" and level > 0 then return math.floor(level) end
end

local function keepLevel(questId)
    questId = tonumber(questId)
    if questId then levels[questId] = readLevel(questId) or levels[questId] end
end

local function remember()
    window = { title = ns.Call("GetTitleText"), giver = giver(), time = GetTime() }
    keepLevel(ns.Call("GetQuestID"))
end
ns.On("QUEST_PROGRESS", remember, "Quest.window")
ns.On("QUEST_COMPLETE", remember, "Quest.window")
ns.On("QUEST_ACCEPTED", function(first, second) keepLevel(second or first) end, "Quest.accepted")

local function questName(questId)
    local log = C_QuestLog
    if type(log) == "table" then
        if log.GetTitleForQuestID then
            local ok, title = pcall(log.GetTitleForQuestID, questId)
            if ok and title then return title end
        end
        if log.GetQuestInfo then
            local ok, title = pcall(log.GetQuestInfo, questId)
            if ok and title then return title end
        end
    end
end

ns.On("QUEST_TURNED_IN", function(questId, xp, copper)
    if not ns.Enabled("quest") then return end
    questId = tonumber(questId)
    if not questId then return end
    local recent = window and GetTime() - window.time <= WINDOW and window or nil
    keepLevel(questId)
    local questLevel = levels[questId]
    levels[questId] = nil
    ns.AddHistory("quest", {
        at = time(),
        questId = questId,
        name = ns.Clean(questName(questId) or (recent and recent.title), 80),
        questLevel = questLevel,
        xp = tonumber(xp) or 0,
        copper = tonumber(copper) or 0,
        giver = recent and recent.giver or { kind = "unknown" },
        zone = ns.Clean(ns.Call("GetRealZoneText"), 60),
        subzone = ns.Clean(ns.Call("GetSubZoneText"), 60),
    }, "quest:" .. questId)
    ns.Debug("quest " .. questId .. " turned in")
end, "Quest.turnedIn")
