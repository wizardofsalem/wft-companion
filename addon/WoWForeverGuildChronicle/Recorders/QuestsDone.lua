local _, ns = ...

local DELAY = 5

local function completedIds()
    local log = C_QuestLog
    if type(log) == "table" and type(log.GetAllCompletedQuestIDs) == "function" then
        local ok, ids = pcall(log.GetAllCompletedQuestIDs)
        if ok and type(ids) == "table" then return ids end
    end
    local done = ns.Call("GetQuestsCompleted")
    if type(done) ~= "table" then return nil end
    local ids = {}
    for id, yes in pairs(done) do
        if yes then ids[#ids + 1] = id end
    end
    return ids
end

local function capture()
    if not ns.Enabled("questsDone") then return end
    local c = ns.Character()
    local ids = completedIds()
    if not c or not ids or #ids == 0 then return end
    local out = {}
    for _, id in ipairs(ids) do
        if tonumber(id) then out[#out + 1] = tonumber(id) end
    end
    table.sort(out)
    if ns.SetState("questsDone", ns.StateKey(c), { count = #out, questIds = out }) then
        ns.Debug("quests done: " .. #out)
    end
end

local function later()
    if C_Timer then C_Timer.After(DELAY, capture) else capture() end
end

ns.On("PLAYER_ENTERING_WORLD", later, "QuestsDone.login")
ns.On("QUEST_TURNED_IN", later, "QuestsDone.turnedIn")
