local _, ns = ...

local function pvpRank(raw)
    raw = tonumber(raw)
    if not raw or raw < 5 then return 0 end
    return raw - 4
end

local function n(v) return tonumber(v) or 0 end

local function stats(name, ...)
    if not ns.Func(name) then return nil end
    local r = { ns.Call(name, ...) }
    if r[1] == nil then return nil end
    return r
end

local function capture(why)
    if not ns.Enabled("honor") then return end
    local c = ns.Character()
    if not c then return end
    local fields, any = {}, false
    local classic = ns.Func("UnitPVPRank") ~= nil
    local rank = stats("UnitPVPRank", "player")
    if rank then fields.rank = pvpRank(rank[1]); any = true end
    local progress = stats("GetPVPRankProgress")
    if progress and tonumber(progress[1]) then fields.rankProgress = math.floor(progress[1] * 1000 + 0.5) / 1000 end
    local today = stats("GetPVPSessionStats")
    if today then
        if classic then
            fields.today = { hk = n(today[1]), honor = n(today[2]) }
        else
            fields.today = { hk = n(today[1]), dk = n(today[2]) }
        end
        any = true
    end
    local y = stats("GetPVPYesterdayStats")
    if y then
        fields.yesterday = { hk = n(y[1]), dk = n(y[2]) }
        if classic then fields.yesterday.honor = n(y[3]) end
        any = true
    end
    local w = stats("GetPVPThisWeekStats")
    if w then fields.thisWeek = { hk = n(w[1]), honor = n(w[2]) }; any = true end
    local l = stats("GetPVPLastWeekStats")
    if l then fields.lastWeek = { hk = n(l[1]), dk = n(l[2]), honor = n(l[3]), standing = n(l[4]) }; any = true end
    local t = stats("GetPVPLifetimeStats")
    if t then
        local hk = n(t[1])
        local old = ns.db and ns.db.state and ns.db.state.honor and ns.db.state.honor[ns.StateKey(c)]
        local before = type(old) == "table" and type(old.lifetime) == "table" and tonumber(old.lifetime.hk)
        if before and before > hk then hk = before end
        fields.lifetime = { hk = hk }
        local raw = classic and t[3] or t[2]
        raw = tonumber(raw)
        if classic then fields.lifetime.dk = n(t[2]) end
        if raw and raw >= 0 and raw <= 18 and (rank or not classic) then
            fields.lifetime.highestRank = pvpRank(raw)
        end
        local rankBefore = type(old) == "table" and type(old.lifetime) == "table" and tonumber(old.lifetime.highestRank)
        if rankBefore and rankBefore > (fields.lifetime.highestRank or -1) then
            fields.lifetime.highestRank = rankBefore
        end
        any = true
    end
    if ns.Func("UnitHonorLevel") then
        local level = tonumber((ns.Call("UnitHonorLevel", "player")))
        if level and not ns.IsSecret(level) and level >= 0 and level < 1000 then fields.honorLevel = math.floor(level) end
    end
    if not any and not fields.honorLevel then return end
    if ns.SetState("honor", ns.StateKey(c), fields) then
        ns.Debug("honor (" .. why .. "): " .. (fields.rank and ("R" .. fields.rank) or "no rank") .. ", "
            .. (fields.thisWeek and (fields.thisWeek.hk .. " HKs this week") or "no weekly stats"))
    end
end

ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    capture("login")
    if (isLogin or isReload) and type(C_Timer) == "table" and type(C_Timer.After) == "function" then
        C_Timer.After(15, function() capture("login+15s") end)
        C_Timer.After(90, function() capture("login+90s") end)
    end
end, "Honor.login")
ns.On("PLAYER_PVP_KILLS_CHANGED", function() capture("kill") end, "Honor.kill")
ns.On("PLAYER_PVP_RANK_CHANGED", function() capture("rank") end, "Honor.rank")
ns.On("PLAYER_LOGOUT", function() capture("logout") end, "Honor.logout")
