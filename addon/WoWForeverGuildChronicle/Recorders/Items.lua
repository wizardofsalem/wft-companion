local _, ns = ...

local FORMATS = {
    { "LOOT_ITEM_SELF_MULTIPLE", "loot" },
    { "LOOT_ITEM_SELF", "loot" },
    { "LOOT_ITEM_PUSHED_SELF_MULTIPLE", "push" },
    { "LOOT_ITEM_PUSHED_SELF", "push" },
    { "LOOT_ITEM_CREATED_SELF_MULTIPLE", "craft" },
    { "LOOT_ITEM_CREATED_SELF", "craft" },
}
local patterns
local wonPattern
local won = {}
local mailPending = {}
local tradeItems = {}

local ROLL_WINDOW = 120
local MAIL_WINDOW = 2

local function buildPatterns()
    patterns = {}
    for _, f in ipairs(FORMATS) do
        local pattern = ns.FormatToPattern(_G[f[1]])
        if pattern then
            patterns[#patterns + 1] = { pattern = pattern, kind = f[2] }
        else
            ns.Log("warn", "format string " .. f[1] .. " missing")
        end
    end
    wonPattern = ns.FormatToPattern(_G.LOOT_ROLL_YOU_WON)
end

local function npcSource()
    local kind, id = ns.ParseGUID(ns.Call("UnitGUID", "npc"))
    if kind ~= "creature" then return { kind = "unknown" } end
    return { kind = "creature", id = id, name = ns.Clean(ns.Call("UnitName", "npc"), 60) }
end

local function record(link, count, how, source)
    local itemId = ns.ItemIdFromLink(link)
    if not itemId then return end
    if ns.NoteItem then ns.NoteItem(itemId, link) end
    local _, _, quality = ns.Call("GetItemInfo", link)
    local setId, setName, setPieces = ns.ItemSet(itemId, link)
    if how == "craft" and not source and ns.CraftProfession then
        local profession, professionId = ns.CraftProfession()
        if profession or professionId then
            source = { kind = "unknown", profession = profession, professionId = professionId }
        end
    end
    ns.AddHistory("loot", {
        setId = setId, setName = setName, setPieces = setPieces,
        context = ns.ItemContext(link),
        at = time(),
        itemId = itemId,
        name = ns.Clean(ns.ItemNameFromLink(link) or ns.Call("GetItemInfo", link), 80),
        quality = quality,
        count = count or 1,
        how = how,
        source = source or { kind = "unknown" },
        zone = ns.Clean(ns.Call("GetRealZoneText"), 60),
        subzone = ns.Clean(ns.Call("GetSubZoneText"), 60),
    }, how .. ":" .. itemId .. ":" .. ns.NextSeq())
    ns.Debug("item " .. itemId .. " x" .. tostring(count) .. " via " .. how)
end

function ns.MasterLoot()
    if type(C_PartyInfo) == "table" and C_PartyInfo.GetLootMethod then
        local method = C_PartyInfo.GetLootMethod()
        local enum = type(Enum) == "table" and Enum.LootMethod
        return method == "master" or method == (enum and enum.Masterlooter or 2)
    end
    if type(GetLootMethod) == "function" then return GetLootMethod() == "master" end
    return false
end

local function howLooted(itemId)
    if won[itemId] and GetTime() - won[itemId] <= ROLL_WINDOW then
        won[itemId] = nil
        return "roll"
    end
    if ns.MasterLoot() then return "master" end
    return "loot"
end

local function takeMailPending(itemId)
    for i, p in ipairs(mailPending) do
        if p.itemId == itemId then
            table.remove(mailPending, i)
            return true
        end
    end
    return false
end

local function onChat(line)
    if not ns.Enabled("loot") then return end
    if not patterns then buildPatterns() end
    local wonLink = ns.MatchItemLine(line, wonPattern)
    if wonLink then
        won[ns.ItemIdFromLink(wonLink) or 0] = GetTime()
        return
    end
    for _, p in ipairs(patterns) do
        local link, count = ns.MatchItemLine(line, p.pattern)
        if link then
            local itemId = ns.ItemIdFromLink(link)
            if p.kind == "craft" then
                record(link, count, "craft")
            elseif p.kind == "loot" then
                if not ns.LootWindowHas(itemId) then record(link, count, howLooted(itemId)) end
            else
                local context = ns.Context()
                local fromMail = takeMailPending(itemId)
                if context == "trade" then
                    return
                elseif context == "mail" or fromMail then
                    record(link, count, "mail")
                elseif context == "vendor" then
                    record(link, count, "vendor", npcSource())
                elseif context == "quest" then
                    record(link, count, "quest", npcSource())
                else
                    record(link, count, "unknown")
                end
            end
            return
        end
    end
end

ns.On("CHAT_MSG_LOOT", onChat, "Items.chat")

local function flushMail()
    local t = GetTime()
    local keep = {}
    for _, p in ipairs(mailPending) do
        if t - p.at >= MAIL_WINDOW then
            record(p.link, p.count, "mail")
        else
            keep[#keep + 1] = p
        end
    end
    mailPending = keep
end

local function onTakeMail(index, attachment)
    if not ns.Enabled("loot") then return end
    local first, last = attachment, attachment
    if not attachment then first, last = 1, ATTACHMENTS_MAX_RECEIVE or 12 end
    for a = first, last do
        local link = ns.Call("GetInboxItemLink", index, a)
        if link then
            local _, _, _, count = ns.Call("GetInboxItem", index, a)
            mailPending[#mailPending + 1] = { link = link, itemId = ns.ItemIdFromLink(link), count = count or 1, at = GetTime() }
        end
    end
    if #mailPending > 0 and C_Timer then C_Timer.After(MAIL_WINDOW + 0.1, flushMail) end
end

if type(hooksecurefunc) == "function" then
    if type(TakeInboxItem) == "function" then hooksecurefunc("TakeInboxItem", onTakeMail) end
    if type(AutoLootMailItem) == "function" then
        hooksecurefunc("AutoLootMailItem", function(index) onTakeMail(index, nil) end)
    end
end

ns.On("UI_ERROR_MESSAGE", function()
    if #mailPending > 0 and ns.Active("mail", 0) then
        ns.Debug("mail take failed; dropping " .. #mailPending .. " pending")
        mailPending = {}
    end
end, "Items.mailError")

local tradeGiven, tradeMoney = {}, {}
local function readTrade()
    tradeItems = {}
    for i = 1, 6 do
        local link = ns.Call("GetTradeTargetItemLink", i)
        if link then
            local _, _, count = ns.Call("GetTradeTargetItemInfo", i)
            tradeItems[#tradeItems + 1] = { link = link, count = count or 1 }
        end
    end
    tradeGiven = {}
    for i = 1, 6 do
        local id = ns.ItemIdFromLink(ns.Call("GetTradePlayerItemLink", i))
        if id then tradeGiven[#tradeGiven + 1] = id end
    end
    tradeMoney = { given = tonumber((ns.Call("GetPlayerTradeMoney"))), received = tonumber((ns.Call("GetTargetTradeMoney"))) }
end

ns.On("TRADE_TARGET_ITEM_CHANGED", readTrade, "Items.trade")
ns.On("TRADE_ACCEPT_UPDATE", readTrade, "Items.trade")
ns.On("TRADE_SHOW", function()
    tradeItems, tradeGiven, tradeMoney = {}, {}, {}
end, "Items.trade")
ns.On("UI_INFO_MESSAGE", function(a, b)
    if not ERR_TRADE_COMPLETE or (a ~= ERR_TRADE_COMPLETE and b ~= ERR_TRADE_COMPLETE) then return end
    if ns.Enabled("loot") then
        for _, t in ipairs(tradeItems) do record(t.link, t.count, "trade") end
    end
    if ns.Moment and ns.Enabled("moments") then
        local received = {}
        for _, t in ipairs(tradeItems) do
            local id = ns.ItemIdFromLink(t.link)
            if id then received[#received + 1] = id end
        end
        ns.TradeDone(tradeMoney.given, tradeMoney.received, #tradeGiven > 0 and tradeGiven or nil, #received > 0 and received or nil)
    end
    tradeItems, tradeGiven, tradeMoney = {}, {}, {}
end, "Items.trade")
