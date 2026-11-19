local _, ns = ...

ns.VERSION_PREFIX = "WFTGC"
local REPEAT = 15 * 60
local REPLY_GAP = 60

function ns.ParseVersion(v)
    if type(v) ~= "string" or #v > 16 then return nil end
    local a, b, c = v:match("^(%d%d?%d?%d?)%.(%d%d?%d?%d?)%.(%d%d?%d?%d?)$")
    if not a then return nil end
    return { tonumber(a), tonumber(b), tonumber(c) }
end

function ns.CompareVersions(x, y)
    for i = 1, 3 do
        if x[i] ~= y[i] then return x[i] < y[i] and -1 or 1 end
    end
    return 0
end

local mine
local wantSay = false
local ready = false
local TRIES = 3
local tries = 0
local heardAt = -1
local replyAt
local lastReply = -math.huge
local newest
local ticker

local function now() return type(GetTime) == "function" and GetTime() or 0 end

local function canSend()
    if not ns.Call("IsInGuild") then return false end
    if ns.Call("IsInInstance") then return false end
    if ns.Call("InCombatLockdown") then return false end
    if ns.Call("C_ChatInfo.InChatMessagingLockdown") then return false end
    return true
end

local function send(why)
    local result = ns.Call("C_ChatInfo.SendAddonMessage", ns.VERSION_PREFIX, "V1:" .. ns.VERSION, "GUILD")
    ns.Log("info", "version sent (" .. why .. "): " .. tostring(result))
    return result == nil or result == true or result == 0
end

local function trySay()
    if not ready or not wantSay or not mine or not canSend() then return end
    wantSay = false
    tries = tries + 1
    if send("login") then return end
    if tries < TRIES then
        wantSay = true
    else
        ns.Log("info", "version not sent: refused " .. tries .. " times, given up until the next login or reload")
    end
end

local logged = {}
local function note(v, cmp)
    if logged[v] then return end
    logged[v] = true
    ns.Log("info", "version heard: " .. v .. " (" .. (cmp > 0 and "newer" or cmp < 0 and "older" or "same") .. ")")
end

local function notice()
    if newest then
        ns.Print("A new version of WoW Forever Guild Chronicle (" .. newest .. ") is out. Update it from CurseForge.")
    end
end

local function heard(text)
    local v = type(text) == "string" and text:match("^V1:(.+)$")
    local theirs = ns.ParseVersion(v)
    if not theirs then return end
    local cmp = ns.CompareVersions(theirs, mine)
    note(v, cmp)
    if cmp >= 0 then heardAt = now() end
    if cmp > 0 then
        if not newest or ns.CompareVersions(theirs, ns.ParseVersion(newest)) > 0 then newest = v end
        if not ticker then
            ns.Log("info", "a newer version is out: " .. v)
            notice()
            if type(C_Timer) == "table" and type(C_Timer.NewTicker) == "function" then
                ticker = C_Timer.NewTicker(REPEAT, notice)
            else
                ticker = true
            end
        end
    elseif cmp < 0 and ready and not replyAt and now() - lastReply >= REPLY_GAP then
        local planned = now()
        replyAt = planned
        local function answer()
            replyAt = nil
            if not ready or heardAt > planned or not canSend() then return end
            lastReply = now()
            send("answer")
        end
        if type(C_Timer) == "table" and type(C_Timer.After) == "function" then
            C_Timer.After(math.random(5, 30), answer)
        else
            answer()
        end
    end
end

local listening = false

ns.On("PLAYER_LOGIN", function()
    mine = ns.ParseVersion(ns.VERSION)
    if not mine then return end
    ns.Call("C_ChatInfo.RegisterAddonMessagePrefix", ns.VERSION_PREFIX)
    if not listening then
        listening = true
        ns.On("CHAT_MSG_ADDON", function(prefix, text, channel)
            if not mine or prefix ~= ns.VERSION_PREFIX or channel ~= "GUILD" then return end
            heard(text)
        end, "Version.heard")
    end
    wantSay, tries = true, 0
    local function go() ready = true; trySay() end
    if type(C_Timer) == "table" and type(C_Timer.After) == "function" then
        C_Timer.After(15, go)
    else
        go()
    end
end, "Version.login")

for _, event in ipairs({ "PLAYER_ENTERING_WORLD", "PLAYER_REGEN_ENABLED", "PLAYER_GUILD_UPDATE" }) do
    ns.On(event, trySay, "Version.retry")
end
