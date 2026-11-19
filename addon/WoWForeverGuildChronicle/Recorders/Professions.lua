local _, ns = ...

local PROFESSIONS = {
    Alchemy = true, Blacksmithing = true, Enchanting = true, Engineering = true,
    Herbalism = true, Leatherworking = true, Mining = true, Skinning = true,
    Tailoring = true, Cooking = true, ["First Aid"] = true, Fishing = true,
}
local HEADERS = { Professions = true, ["Secondary Skills"] = true }

local function storeRecipes(profession, rank, maxRank, recipes, professionId)
    local c = ns.Character()
    if not profession or not c then return end
    local charKey = ns.StateKey(c)
    ns.db.recipes[charKey] = ns.db.recipes[charKey] or {}
    profession = ns.Clean(profession, 40)
    local known = ns.db.recipes[charKey][profession] or { recipes = {} }
    local byName = {}
    for _, r in ipairs(known.recipes) do byName[r.name] = r end
    for _, r in ipairs(recipes) do
        if not byName[r.name] then known.recipes[#known.recipes + 1] = r end
        byName[r.name] = r
    end
    for i, r in ipairs(known.recipes) do known.recipes[i] = byName[r.name] end
    known.skill, known.maxSkill = rank, maxRank
    known.professionId = professionId or known.professionId
    ns.db.recipes[charKey][profession] = known
end

local function retailTradeSkill()
    if not (ns.Func("C_TradeSkillUI.GetBaseProfessionInfo") and ns.Func("C_TradeSkillUI.GetAllRecipeIDs")
        and ns.Func("C_TradeSkillUI.GetRecipeInfo")) then
        return nil
    end
    for _, f in ipairs({ "IsTradeSkillLinked", "IsTradeSkillGuild", "IsNPCCrafting", "IsRuneforging" }) do
        if ns.Func("C_TradeSkillUI." .. f) and ns.Call("C_TradeSkillUI." .. f) then
            ns.Debug("professions: someone else's window (" .. f .. "), skipped")
            return false
        end
    end
    local info = ns.Call("C_TradeSkillUI.GetBaseProfessionInfo")
    if type(info) ~= "table" or type(info.professionName) ~= "string" or info.professionName == "" then return false end
    local rank, maxRank = tonumber(info.skillLevel), tonumber(info.maxSkillLevel)
    if (maxRank or 0) == 0 and ns.Func("C_TradeSkillUI.GetChildProfessionInfo") then
        local child = ns.Call("C_TradeSkillUI.GetChildProfessionInfo")
        if type(child) == "table" and (tonumber(child.maxSkillLevel) or 0) > 0 then
            rank, maxRank = tonumber(child.skillLevel), tonumber(child.maxSkillLevel)
        end
    end
    local ids = ns.Call("C_TradeSkillUI.GetAllRecipeIDs")
    local recipes = {}
    for _, id in ipairs(type(ids) == "table" and ids or {}) do
        local r = ns.Call("C_TradeSkillUI.GetRecipeInfo", id)
        if type(r) == "table" and r.learned and type(r.name) == "string" and #recipes < 2000 then
            local recipe = { name = ns.Clean(r.name, 80), spellId = tonumber(r.recipeID) or tonumber(id) }
            local out = ns.Func("C_TradeSkillUI.GetRecipeOutputItemData")
                and ns.Call("C_TradeSkillUI.GetRecipeOutputItemData", id)
            recipe.itemId = type(out) == "table" and tonumber(out.itemID) or nil
            if not recipe.itemId and ns.Func("C_TradeSkillUI.GetRecipeItemLink") then
                recipe.itemId = ns.ItemIdFromLink(ns.Call("C_TradeSkillUI.GetRecipeItemLink", id))
            end
            recipes[#recipes + 1] = recipe
        end
    end
    local unlearned = 0
    for _, id in ipairs(type(ids) == "table" and ids or {}) do
        local r = ns.Call("C_TradeSkillUI.GetRecipeInfo", id)
        if type(r) == "table" and not r.learned then unlearned = unlearned + 1 end
    end
    ns.Debug("professions: window " .. tostring(info.professionName) .. " id " .. tostring(info.professionID)
        .. ", " .. (type(ids) == "table" and #ids or -1) .. " recipe IDs (" .. #recipes .. " learned, "
        .. unlearned .. " not), first " .. tostring(recipes[1] and recipes[1].name) .. " item "
        .. tostring(recipes[1] and recipes[1].itemId))
    return info.professionName, rank, maxRank, recipes, tonumber(info.professionID)
end

local windowProfession, windowProfessionId
local lastCast = { at = -1e9 }
local CAST_WINDOW = 10

function ns.CraftProfession()
    if lastCast.profession and GetTime() - lastCast.at <= CAST_WINDOW then return lastCast.profession, lastCast.professionId end
    return windowProfession, windowProfessionId
end

local function readTradeSkill()
    if not ns.Enabled("professions") then return end
    if not ns.Func("GetTradeSkillLine") then
        local profession, rank, maxRank, recipes, professionId = retailTradeSkill()
        if profession == nil then ns.NoteMissing("GetTradeSkillLine") end
        if not profession then return end
        ns.Debug("professions: " .. profession .. " " .. tostring(rank) .. "/" .. tostring(maxRank)
            .. ", " .. #recipes .. " recipes (C_TradeSkillUI)")
        storeRecipes(profession, rank, maxRank, recipes, professionId)
        windowProfession = ns.Clean(profession, 40)
        windowProfessionId = professionId
        ns.CaptureProfessions("trade skill window")
        return
    end
    local profession, rank, maxRank = ns.Call("GetTradeSkillLine")
    if not profession or profession == "UNKNOWN" then return end
    local recipes = {}
    for i = 1, ns.Call("GetNumTradeSkills") or 0 do
        local name, kind = ns.Call("GetTradeSkillInfo", i)
        if name and kind ~= "header" then
            local recipe = { name = ns.Clean(name, 80) }
            recipe.itemId = ns.ItemIdFromLink(ns.Call("GetTradeSkillItemLink", i))
            recipe.spellId = ns.SpellIdFromLink(ns.Call("GetTradeSkillRecipeLink", i))
            recipes[#recipes + 1] = recipe
        end
    end
    windowProfession, windowProfessionId = ns.Clean(profession, 40), nil
    ns.Debug("professions: trade skill " .. tostring(profession) .. " " .. tostring(rank) .. "/" .. tostring(maxRank)
        .. ", " .. #recipes .. " recipes")
    storeRecipes(profession, rank, maxRank, recipes)
    ns.CaptureProfessions("trade skill window")
end

local function readCraft()
    if not ns.Enabled("professions") then return end
    local profession, rank, maxRank = ns.Call("GetCraftDisplaySkillLine")
    if not profession then
        ns.Debug("professions: craft window without a skill line")
        return
    end
    local recipes = {}
    for i = 1, ns.Call("GetNumCrafts") or 0 do
        local name, _, kind = ns.Call("GetCraftInfo", i)
        if name and kind ~= "header" then
            local recipe = { name = ns.Clean(name, 80) }
            local link = ns.Call("GetCraftItemLink", i)
            recipe.itemId = ns.ItemIdFromLink(link)
            recipe.spellId = ns.SpellIdFromLink(link) or ns.SpellIdFromLink(ns.Call("GetCraftRecipeLink", i))
            recipes[#recipes + 1] = recipe
        end
    end
    windowProfession, windowProfessionId = ns.Clean(profession, 40), nil
    ns.Debug("professions: craft " .. tostring(profession) .. ", " .. #recipes .. " recipes")
    storeRecipes(profession, rank, maxRank, recipes)
    ns.CaptureProfessions("craft window")
end

local function skill()
    if ns.Func("GetNumSkillLines") and ns.Func("GetSkillLineInfo") then
        return {
            num = function() return ns.Call("GetNumSkillLines") end,
            at = function(i)
                local name, isHeader, isExpanded, rank, _, _, maxRank = ns.Call("GetSkillLineInfo", i)
                return name, isHeader, isExpanded, rank, maxRank
            end,
            expand = function(i) ns.Call("ExpandSkillHeader", i) end,
            collapse = function(i) ns.Call("CollapseSkillHeader", i) end,
        }
    end
    if ns.Func("C_SkillInfo.GetNumSkillLines") and ns.Func("C_SkillInfo.GetSkillLineInfo") then
        return {
            num = function() return ns.Call("C_SkillInfo.GetNumSkillLines") end,
            at = function(i)
                local d = ns.Call("C_SkillInfo.GetSkillLineInfo", i)
                if type(d) ~= "table" then return nil end
                return d.name, d.isHeader, not d.isCollapsed, d.rank, d.maxRank,
                    tonumber(d.skillID), tonumber(d.parentSkillLineID)
            end,
            expand = function(i) ns.Call("C_SkillInfo.ExpandSkillHeader", i) end,
            collapse = function(i) ns.Call("C_SkillInfo.CollapseSkillHeader", i) end,
        }
    end
    ns.NoteMissing(ns.Func("GetNumSkillLines") and "GetSkillLineInfo" or "GetNumSkillLines")
end

local loggedLines = {}

local function skillLines()
    local sk = skill()
    if not sk then return {} end
    local opened = {}
    local i = 1
    while i <= (sk.num() or 0) and i < 200 do
        local name, isHeader, isExpanded = sk.at(i)
        if isHeader and not isExpanded and name then
            sk.expand(i)
            opened[name] = true
        end
        i = i + 1
    end

    local found, byName, header = {}, {}, nil
    for j = 1, math.min(sk.num() or 0, 200) do
        local name, isHeader, _, rank, maxRank, skillId, parentId = sk.at(j)
        if isHeader then
            header = name
        elseif name and (PROFESSIONS[name] or HEADERS[header]) then
            local clean = ns.Clean(name, 40)
            local id = (parentId and parentId > 0) and parentId or skillId
            local line = byName[clean]
            if not line then
                line = { name = clean, skill = rank, maxSkill = maxRank, professionId = id }
                byName[clean] = line
                found[#found + 1] = line
            else
                if not line.professionId then line.professionId = id end
                if (tonumber(rank) or 0) > (tonumber(line.skill) or 0) then
                    line.skill, line.maxSkill = rank, maxRank
                end
            end
            local key = clean .. "#" .. tostring(skillId)
            if not loggedLines[key] then
                loggedLines[key] = true
                ns.Debug("professions: skill line " .. clean .. " id " .. tostring(skillId)
                    .. " parent " .. tostring(parentId) .. " rank " .. tostring(rank)
                    .. "/" .. tostring(maxRank))
            end
        end
    end

    for j = (sk.num() or 0), 1, -1 do
        local name, isHeader, isExpanded = sk.at(j)
        if isHeader and isExpanded and opened[name] then
            sk.collapse(j)
        end
    end
    return found
end

function ns.CaptureProfessions(why)
    if ns.InCombat() then return ns.AfterCombat("professions:" .. tostring(why), function() ns.CaptureProfessions(why) end) end
    if not ns.Enabled("professions") then return end
    local c = ns.Character()
    if not c then return end
    local known = ns.db.recipes[ns.StateKey(c)] or {}
    local professions, have = {}, {}
    local lines = skillLines()
    local stored = ns.db.state.professions and ns.db.state.professions[ns.StateKey(c)]
    if #lines == 0 and stored then
        ns.Debug("professions (" .. why .. "): empty skill list; keeping the stored record")
        return
    end
    for _, line in ipairs(lines) do
        local seen = known[line.name]
        line.recipes = seen and ns.Copy(seen.recipes) or {}
        line.professionId = (seen and seen.professionId) or line.professionId
        professions[#professions + 1] = line
        have[line.name] = true
    end
    local extra = {}
    for name, seen in pairs(known) do
        if not have[name] and seen.skill then
            extra[#extra + 1] = { name = name, skill = seen.skill, maxSkill = seen.maxSkill,
                professionId = seen.professionId, recipes = ns.Copy(seen.recipes) }
        end
    end
    table.sort(extra, function(a, b) return a.name < b.name end)
    for _, p in ipairs(extra) do professions[#professions + 1] = p end
    ns.Debug("professions (" .. why .. "): "
        .. #lines .. " professions in them, " .. #professions .. " stored")
    if #professions > 0 then
        ns.SetState("professions", ns.StateKey(c), { professions = professions })
    end
end

local pending = false
local function readSoon()
    if not C_Timer or ns.Func("GetTradeSkillLine") then return readTradeSkill() end
    if pending then return end
    pending = true
    C_Timer.After(1, function() pending = false; readTradeSkill() end)
end

ns.On("TRADE_SKILL_CLOSE", function() windowProfession, windowProfessionId = nil, nil end, "Professions.close")
ns.On("CRAFT_CLOSE", function() windowProfession, windowProfessionId = nil, nil end, "Professions.close")
ns.On("UNIT_SPELLCAST_SUCCEEDED", function(unit, _, spellId)
    if unit ~= "player" or ns.IsSecret(spellId) or not tonumber(spellId) or not ns.db then return end
    local c = ns.Character()
    local known = c and ns.db.recipes[ns.StateKey(c)]
    for profession, p in pairs(known or {}) do
        for _, r in ipairs(p.recipes or {}) do
            if r.spellId == tonumber(spellId) then
                lastCast.profession, lastCast.professionId, lastCast.at = profession, p.professionId, GetTime()
                return
            end
        end
    end
end, "Professions.cast")
ns.On("TRADE_SKILL_SHOW", readSoon, "Professions.readTradeSkill")
ns.On("TRADE_SKILL_LIST_UPDATE", readSoon, "Professions.readTradeSkill")
ns.On("TRADE_SKILL_UPDATE", readTradeSkill, "Professions.readTradeSkill")
ns.On("CRAFT_SHOW", readCraft, "Professions.readCraft")
ns.On("CRAFT_UPDATE", readCraft, "Professions.readCraft")
ns.On("PLAYER_LOGIN", function()
    if C_Timer then C_Timer.After(5, function() ns.CaptureProfessions("login") end) end
end, "Professions.login")
ns.On("SKILL_LINES_CHANGED", function() ns.CaptureProfessions("skills changed") end, "Professions.skills")
ns.On("PLAYER_LOGOUT", function() ns.CaptureProfessions("logout") end, "Professions.capture")
