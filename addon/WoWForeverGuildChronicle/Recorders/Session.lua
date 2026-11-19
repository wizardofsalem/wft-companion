local _, ns = ...

local CHECK = 15 * 60
local KEEP = 30 * 86400
local ticker

local function client()
    local version, build, _, interface = ns.Call("GetBuildInfo")
    local project = type(WOW_PROJECT_ID) == "number" and WOW_PROJECT_ID or nil
    local cap = tonumber((ns.Call("GetMaxLevelForPlayerExpansion"))) or tonumber((ns.Call("GetMaxPlayerLevel")))
    return { version = ns.Clean(version, 20), build = ns.Clean(build, 20), interface = tonumber(interface),
        projectId = project, locale = ns.Clean((ns.Call("GetLocale")), 10), maxLevel = cap }
end

local function stateKey(c, loginAt) return ns.StateKey(c) .. "|" .. loginAt end

local function write(c, s)
    if not ns.Enabled("session") then return end
    local last = s.logoutAt or s.lastSeenAt or s.loginAt
    ns.SetState("session", stateKey(c, s.loginAt), {
        loginAt = s.loginAt, logoutAt = s.logoutAt, lastSeenAt = s.lastSeenAt,
        seconds = math.max(0, last - s.loginAt), ["end"] = s["end"], reloads = s.reloads,
        client = s.client,
    })
end

local function prune(c)
    local bucket = ns.db.state.session
    if not bucket then return end
    local prefix, cutoff = ns.StateKey(c) .. "|", time() - KEEP
    for key, r in pairs(bucket) do
        if key:sub(1, #prefix) == prefix and (tonumber(r.loginAt) or 0) < cutoff then bucket[key] = nil end
    end
end

function ns.SessionId()
    local c = ns.Character()
    local s = c and ns.db and ns.db.openSession and ns.db.openSession[ns.StateKey(c)]
    return s and s.loginAt
end

local function checkpoint()
    local c = ns.Character()
    local s = c and ns.db.openSession and ns.db.openSession[ns.StateKey(c)]
    if not s or s.logoutAt then return end
    s.lastSeenAt = time()
    write(c, s)
end

ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    if not isLogin and not isReload then return end
    local c = ns.Character()
    if not c then return end
    ns.db.openSession = ns.db.openSession or {}
    local key = ns.StateKey(c)
    local s = ns.db.openSession[key]
    local now = time()
    if isReload and s and (not s.logoutAt or now - s.logoutAt < CHECK) then
        s.logoutAt, s["end"] = nil, nil
        s.reloads = (s.reloads or 0) + 1
        s.lastSeenAt = now
    else
        if s and not s.logoutAt then
            s["end"] = "gap"
            write(c, s)
        end
        prune(c)
        s = { loginAt = now, lastSeenAt = now, reloads = 0, client = client() }
        ns.db.openSession[key] = s
    end
    write(c, s)
    if not ticker and type(C_Timer) == "table" and type(C_Timer.NewTicker) == "function" then
        ticker = C_Timer.NewTicker(CHECK, checkpoint)
    end
end, "Session.start")

ns.On("PLAYER_LOGOUT", function()
    local c = ns.Character()
    local s = c and ns.db.openSession and ns.db.openSession[ns.StateKey(c)]
    if not s then return end
    s.logoutAt, s.lastSeenAt, s["end"] = time(), time(), "logout"
    write(c, s)
end, "Session.logout")
