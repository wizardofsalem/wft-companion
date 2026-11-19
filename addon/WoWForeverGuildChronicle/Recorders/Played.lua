local _, ns = ...

local INTERVAL = 30 * 60
local SAFETY = 5
local pending = false
local muted = {}
local ding

local function relisten()
    for _, f in ipairs(muted) do f:RegisterEvent("TIME_PLAYED_MSG") end
    muted = {}
end

local function unmute()
    pending = false
    ns.AfterCombat("played:unmute", relisten)
end

local function mute()
    for i = 1, NUM_CHAT_WINDOWS or 10 do
        local f = _G["ChatFrame" .. i]
        if f and f.IsEventRegistered and f:IsEventRegistered("TIME_PLAYED_MSG") then
            f:UnregisterEvent("TIME_PLAYED_MSG")
            muted[#muted + 1] = f
        end
    end
end

local function request()
    if ns.InCombat() then return ns.AfterCombat("played", request) end
    if not ns.Enabled("played") or pending or type(RequestTimePlayed) ~= "function" then return end
    pending = true
    ns.Trace("Played.mute")
    mute()
    ns.Trace("RequestTimePlayed")
    RequestTimePlayed()
    C_Timer.After(SAFETY, unmute)
end

function ns.TimeFinishedLevel(level, write)
    if not ns.Enabled("played") then return end
    if ding then
        ding.level, ding.start, ding.asked = level, nil, not pending
        request()
        return
    end
    local c = ns.Character()
    local bucket = ns.db.state.played
    local read = c and bucket and bucket[ns.StateKey(c)]
    local start
    if read and type(read.character) == "table" and read.character.level == level - 1
        and type(read.totalSeconds) == "number" and type(read.levelSeconds) == "number" then
        start = read.totalSeconds - read.levelSeconds
    end
    ding = { level = level, start = start, write = write, asked = not pending }
    request()
end

ns.On("TIME_PLAYED_MSG", function(total, thisLevel)
    if not pending then return end
    unmute()
    if ding and not ding.asked then
        ding.asked = true
        request()
        return
    end
    local c = ns.Character()
    if not c or not total then return end
    local totalSeconds, levelSeconds = math.floor(total), math.floor(thisLevel or 0)
    local r = ns.SetState("played", ns.StateKey(c), {
        totalSeconds = totalSeconds,
        levelSeconds = levelSeconds,
    })
    if not ding then return end
    local done = ding
    ding = nil
    if r and type(r.character) == "table" then r.character.level = done.level end
    local seconds = done.start and totalSeconds - levelSeconds - done.start
    if seconds and seconds > 0 then done.write(seconds) end
end, "Played.TIME_PLAYED_MSG")

ns.On("PLAYER_LOGIN", function()
    C_Timer.After(15, request)
    if C_Timer.NewTicker then C_Timer.NewTicker(INTERVAL, request) end
end, "Played.PLAYER_LOGIN")
