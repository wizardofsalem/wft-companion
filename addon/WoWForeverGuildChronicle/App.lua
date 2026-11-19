local _, ns = ...

ns.UPLOAD_URL = "wowforevertracker.com/upload"
ns.START_URL = "wowforevertracker.com/start"
local FRESH_DAYS = 7

function ns.AppState()
    local a = ns.appStatus
    if type(a) ~= "table" or type(a.seen) ~= "number" or a.seen ~= a.seen then return "none" end
    local version = type(a.version) == "string" and a.version or nil
    if not (time() - a.seen <= FRESH_DAYS * 86400) then return "stale", version, a.seen end
    if a.upload == "signin" then return "signin", version, a.seen end
    return "on", version, a.seen
end

local LOG_OUT = "Your data is saved when you log out or reload"

function ns.AppLine(kind)
    local state, version, seen = ns.AppState()
    local login = kind == "login"
    if state == "on" then
        local on = "Companion app: |cff44ff44on|r" .. (version and (" (v" .. version .. ")") or "") .. ". "
        if login then return on .. LOG_OUT .. ", and the app uploads it each time." end
        return on .. "It uploads your data each time it is saved."
    end
    if state == "signin" then
        return "Companion app: |cffff4444sign in again|r. Open the app and press Sign in; until then your data isn't being uploaded."
    end
    if state == "stale" then
        local gone = "Companion app not seen for " .. math.floor((time() - seen) / 86400)
            .. " days, so your data isn't being uploaded. "
        if login then
            return gone .. "It is saved when you log out or reload, and you can learn more at " .. ns.START_URL .. "."
        end
        return gone .. "Learn more at " .. ns.START_URL .. "."
    end
    if login then
        return LOG_OUT .. ". To update your guild's page, learn more at " .. ns.START_URL .. "."
    end
    return "Learn more at " .. ns.START_URL .. "."
end

ns.On("PLAYER_LOGIN", function()
    local function show()
        if not ns.db then return end
        ns.Print(ns.AppLine("login"))
        ns.Log("info", "app state " .. (ns.AppState()))
    end
    if type(C_Timer) == "table" and type(C_Timer.After) == "function" then
        C_Timer.After(8, show)
    else
        show()
    end
end, "App.notice")
