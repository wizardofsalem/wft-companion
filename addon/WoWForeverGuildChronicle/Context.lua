local _, ns = ...

local open = {}
local recent = {}

local RECENT = 3

local function track(showEvent, closeEvent, name)
    ns.On(showEvent, function() open[name] = true end, "Context." .. name)
    ns.On(closeEvent, function()
        open[name] = nil
        recent[name] = GetTime()
    end, "Context." .. name)
end

track("MERCHANT_SHOW", "MERCHANT_CLOSED", "merchant")
track("MAIL_SHOW", "MAIL_CLOSED", "mail")
local function mailType()
    local e = type(Enum) == "table" and Enum.PlayerInteractionType
    return type(e) == "table" and e.MailInfo or nil
end
local function mailClosed()
    open.mail = nil
    recent.mail = GetTime()
end
ns.On("PLAYER_INTERACTION_MANAGER_FRAME_SHOW", function(kind)
    if kind ~= nil and kind == mailType() then open.mail = true end
end, "Context.mail")
ns.On("PLAYER_INTERACTION_MANAGER_FRAME_HIDE", function(kind)
    if kind ~= nil and kind == mailType() then mailClosed() end
end, "Context.mail")
track("TRADE_SHOW", "TRADE_CLOSED", "trade")
track("AUCTION_HOUSE_SHOW", "AUCTION_HOUSE_CLOSED", "auction")
track("LOOT_OPENED", "LOOT_CLOSED", "loot")
track("QUEST_COMPLETE", "QUEST_FINISHED", "quest")
track("GUILDBANKFRAME_OPENED", "GUILDBANKFRAME_CLOSED", "guildBank")
track("TRAINER_SHOW", "TRAINER_CLOSED", "trainer")

function ns.Mark(name)
    recent[name] = GetTime()
end

function ns.Active(name, within)
    if open[name] then return true end
    return recent[name] ~= nil and GetTime() - recent[name] <= (within or RECENT)
end

function ns.Context()
    if ns.Active("tradeDone") or open.trade then return "trade" end
    if open.mail then return "mail" end
    if open.auction then return "auction" end
    if ns.Active("guildBank", 2) then return "guildBank" end
    if open.trainer then return "trainer" end
    if ns.Active("taxi", 3) then return "taxi" end
    if ns.Active("repair", 1) then return "repair" end
    if open.merchant then return "vendor" end
    if ns.Active("quest") then return "quest" end
    if ns.Active("loot", 1) then return "loot" end
    return "unknown"
end

if type(hooksecurefunc) == "function" then
    if type(_G.CloseMail) == "function" then
        hooksecurefunc("CloseMail", mailClosed)
    else
        ns.NoteMissing("CloseMail")
    end
    for _, fn in ipairs({ "RepairAllItems" }) do
        if type(_G[fn]) == "function" then
            hooksecurefunc(fn, function() ns.Mark("repair") end)
        end
    end
    for _, fn in ipairs({ "DepositGuildBankMoney", "WithdrawGuildBankMoney" }) do
        if type(_G[fn]) == "function" then
            hooksecurefunc(fn, function() ns.Mark("guildBank") end)
        else
            ns.NoteMissing(fn)
        end
    end
end

ns.On("UI_INFO_MESSAGE", function(a, b)
    if ERR_TRADE_COMPLETE and (a == ERR_TRADE_COMPLETE or b == ERR_TRADE_COMPLETE) then ns.Mark("tradeDone") end
end, "Context.trade")
