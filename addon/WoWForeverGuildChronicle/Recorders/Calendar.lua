local _, ns = ...

local DAYS = 14
local KINDS = { GUILD_EVENT = "event", GUILD_ANNOUNCEMENT = "announcement" }

local function stamp(t)
    if type(t) ~= "table" then return nil end
    return string.format("%04d-%02d-%02d %02d:%02d", tonumber(t.year) or 0, tonumber(t.month) or 0,
        tonumber(t.monthDay) or 0, tonumber(t.hour) or 0, tonumber(t.minute) or 0)
end

local function num(v)
    v = tonumber(v)
    if v and not ns.IsSecret(v) then return v end
end

local function capture()
    if ns.InCombat() then return ns.AfterCombat("calendar", function() capture() end) end
    if not ns.Enabled("calendar") or not ns.Call("IsInGuild") then return end
    local cal = C_Calendar
    if type(cal) ~= "table" or type(cal.GetNumDayEvents) ~= "function" or type(cal.GetDayEvent) ~= "function" then
        return
    end
    local guildName = ns.Call("GetGuildInfo", "player")
    local c = ns.Character()
    local now = date("*t", time())
    if not guildName or not c or not now then return end
    local events = {}
    for d = 0, DAYS - 1 do
        local t = date("*t", time() + d * 86400)
        local monthOffset = (t.year - now.year) * 12 + (t.month - now.month)
        local ok, n = pcall(cal.GetNumDayEvents, monthOffset, t.day)
        for i = 1, (ok and tonumber(n) or 0) do
            local okE, e = pcall(cal.GetDayEvent, monthOffset, t.day, i)
            if okE and type(e) == "table" and type(e.startTime) == "table" then
                local kind = KINDS[e.calendarType]
                if not kind and type(e.calendarType) == "string" and not ns.IsSecret(e.calendarType) then
                    kind = ns.Clean(e.calendarType:lower(), 20)
                end
                local ev = { title = ns.Clean(e.title, 80), kind = kind or "other", start = stamp(e.startTime) }
                ev.endsAt = stamp(e.endTime)
                if type(e.invitedBy) == "string" and not ns.IsSecret(e.invitedBy) and e.invitedBy ~= "" then
                    ev.creator = ns.Clean(e.invitedBy, 60)
                end
                if e.calendarType == "HOLIDAY" then ev.holiday = ev.title end
                ev.inviteStatus = num(e.inviteStatus)
                ev.eventType = num(e.eventType)
                if e.isLocked ~= nil and not ns.IsSecret(e.isLocked) then ev.locked = e.isLocked and true or false end
                events[#events + 1] = ev
            end
        end
    end
    if ns.SetState("calendar", guildName .. "-" .. c.realm, { guild = ns.Clean(guildName, 60), events = events }) then
        ns.Debug("calendar: " .. #events .. " guild events")
    end
end

ns.On("CALENDAR_UPDATE_EVENT_LIST", capture, "Calendar.update")
