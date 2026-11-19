local _, ns = ...

local TEXT = "Save your WoW Forever Tracker data now?"
local SAVE_NOW, LATER = "Save now", "Later"
ns.SAVE_AFTER = 45 * 60
local QUIET_AFTER_SAVE = 5 * 60
local QUIET_AFTER_ASK = 10 * 60
local LEAVE_DELAY = 5
local CHECK_EVERY = 60

local lastSave
local lastAsked
local lastKind
local panel
local noPanel

local function now() return type(GetTime) == "function" and GetTime() or 0 end

function ns.SavePromptOn()
    return ns.db ~= nil and ns.db.savePrompt ~= false
end

local function reload()
    if type(C_UI) == "table" and type(C_UI.Reload) == "function" then
        C_UI.Reload()
    elseif type(ReloadUI) == "function" then
        ReloadUI()
    end
end

local function visible()
    return panel ~= nil and panel:IsShown() and true or false
end

local function hide()
    if visible() then panel:Hide() end
end

local function answer(save)
    hide()
    ns.Log("info", save and "save prompt: save now" or "save prompt: later")
    if save then reload() end
end

local function build()
    local f = CreateFrame("Frame", nil, UIParent)
    f:Hide()
    f:SetSize(320, 88)
    f:SetPoint("TOP", UIParent, "TOP", 0, -135)
    f:SetFrameStrata("HIGH")
    f:EnableMouse(true)

    local edge = f:CreateTexture(nil, "BACKGROUND")
    edge:SetAllPoints(f)
    edge:SetColorTexture(0.5, 0.5, 0.5, 1)
    local fill = f:CreateTexture(nil, "BORDER")
    fill:SetPoint("TOPLEFT", f, "TOPLEFT", 1, -1)
    fill:SetPoint("BOTTOMRIGHT", f, "BOTTOMRIGHT", -1, 1)
    fill:SetColorTexture(0.05, 0.05, 0.05, 1)

    local text = f:CreateFontString(nil, "ARTWORK", "GameFontHighlight")
    text:SetPoint("TOP", f, "TOP", 0, -18)
    text:SetWidth(290)
    text:SetText(TEXT)

    local function button(label, save)
        local b = CreateFrame("Button", nil, f, "UIPanelButtonTemplate")
        b:SetSize(120, 22)
        b:SetText(label)
        b:SetScript("OnClick", function() answer(save) end)
        return b
    end
    button(SAVE_NOW, true):SetPoint("BOTTOMRIGHT", f, "BOTTOM", -5, 16)
    button(LATER, false):SetPoint("BOTTOMLEFT", f, "BOTTOM", 5, 16)
    return f
end

local function show()
    if noPanel then return false end
    if not panel then
        local ok, made = pcall(build)
        if not ok or not made then
            noPanel = true
            ns.Log("warn", "save prompt: no panel on this client: " .. tostring(made))
            return false
        end
        panel = made
    end
    panel:Show()
    return true
end

local function instanceKind()
    local _, kind = ns.Call("GetInstanceInfo")
    if type(kind) ~= "string" or ns.IsSecret(kind) then return "none" end
    return kind
end

local function safe()
    if not ns.SavePromptOn() or ns.InCombat() then return false end
    if instanceKind() ~= "none" then return false end
    if ns.Call("UnitIsDeadOrGhost", "player") == true then return false end
    return true
end

local function ask(why)
    if not safe() or visible() or not show() then return false end
    lastAsked = now()
    ns.Log("info", "save prompt shown (" .. why .. ")")
    return true
end

local function tick()
    local t = now()
    if not lastSave or t - lastSave < ns.SAVE_AFTER then return end
    if lastAsked and t - lastAsked < ns.SAVE_AFTER then return end
    if ns.Call("IsResting") ~= true then return end
    ask("timer")
end
ns.SavePromptTick = tick

local function leftDungeon()
    local t = now()
    if lastSave and t - lastSave < QUIET_AFTER_SAVE then return end
    if lastAsked and t - lastAsked < QUIET_AFTER_ASK then return end
    ns.AfterCombat("savePrompt:leave", function() ask("left a dungeon") end)
end

local function zoneCheck()
    local kind = instanceKind()
    local was = lastKind
    lastKind = kind
    if kind ~= "none" then hide() end
    if kind == "none" and (was == "party" or was == "raid") then
        if C_Timer and C_Timer.After then C_Timer.After(LEAVE_DELAY, leftDungeon) else leftDungeon() end
    end
end

ns.On("PLAYER_ENTERING_WORLD", function(isLogin, isReload)
    if isLogin or isReload then lastSave = now() end
    zoneCheck()
end, "SavePrompt.enter")
ns.On("ZONE_CHANGED_NEW_AREA", zoneCheck, "SavePrompt.zone")
ns.On("PLAYER_REGEN_DISABLED", hide, "SavePrompt.combat")
ns.On("PLAYER_LOGIN", function()
    if C_Timer and C_Timer.NewTicker then C_Timer.NewTicker(CHECK_EVERY, function() pcall(tick) end) end
end, "SavePrompt.login")
