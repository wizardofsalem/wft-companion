local _, ns = ...

local KILLER_WINDOW = 10
local playerGUID
local last

local DAMAGE = {
    SWING_DAMAGE = true,
    RANGE_DAMAGE = true,
    SPELL_DAMAGE = true,
    SPELL_PERIODIC_DAMAGE = true,
    ENVIRONMENTAL_DAMAGE = true,
}

local function onCombatLog()
    if not ns.Enabled("death") then return end
    local _, subevent, _, sourceGUID, sourceName, _, _, destGUID, _, _, _, a1, a2 = CombatLogGetCurrentEventInfo()
    if destGUID ~= playerGUID or not DAMAGE[subevent] then return end
    local hit = { time = GetTime(), sourceGUID = sourceGUID, sourceName = sourceName }
    if subevent == "ENVIRONMENTAL_DAMAGE" then
        hit.environment = a1
    elseif subevent ~= "SWING_DAMAGE" then
        hit.spellId, hit.spellName = a1, a2
    end
    last = hit
end

function ns.CombatLogAllowed()
    local interface = select(4, ns.Call("GetBuildInfo"))
    if type(interface) ~= "number" then return true end
    return interface < 16000 or (interface >= 20000 and interface < 110000)
end

local function updateRegistration()
    if ns.Enabled("death") and not ns.CombatLogAllowed() then
        ns.combatLogRegistered = false
        ns.Log("info", "combat log not available to addons on this client: deaths without killers")
        return
    end
    if ns.Enabled("death") then
        ns.combatLogRegistered = pcall(ns.combatFrame.RegisterEvent, ns.combatFrame, "COMBAT_LOG_EVENT_UNFILTERED")
        if not ns.combatLogRegistered then ns.Log("warn", "combat log unavailable: deaths without killers") end
    else
        pcall(ns.combatFrame.UnregisterEvent, ns.combatFrame, "COMBAT_LOG_EVENT_UNFILTERED")
        ns.combatLogRegistered = false
        last = nil
    end
end

ns.combatFrame = CreateFrame("Frame")
ns.combatFrame:SetScript("OnEvent", function()
    if ns.ProbeCombatLog then pcall(ns.ProbeCombatLog) end
    local ok, err = pcall(onCombatLog)
    if not ok then ns.Log("error", "combat log: " .. tostring(err)) end
end)

ns.On("PLAYER_LOGIN", function()
    playerGUID = ns.PlayerGUID()
    updateRegistration()
end, "Death.PLAYER_LOGIN")

ns.On("PLAYER_DEAD", function()
    local killer
    if last and GetTime() - last.time <= KILLER_WINDOW then
        if last.environment then
            killer = { name = ns.Clean(last.environment, 40), kind = "environment" }
        else
            local kind, id = ns.ParseGUID(last.sourceGUID)
            killer = { name = ns.Clean(last.sourceName, 60), kind = kind, id = id }
            if last.spellName then
                killer.spell = ns.Clean(last.spellName, 60)
                if type(last.spellId) == "number" and last.spellId > 0 then
                    killer.spellId = last.spellId
                end
            end
        end
    end
    local at = time()
    ns.AddHistory("death", {
        at = at,
        zone = ns.Clean(ns.Call("GetRealZoneText"), 60),
        subzone = ns.Clean(ns.Call("GetSubZoneText"), 60),
        level = UnitLevel("player"),
        killer = killer,
    }, "death")
    last = nil
end, "Death.PLAYER_DEAD")
