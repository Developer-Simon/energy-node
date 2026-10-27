// Pure permission model for the users page: which permissions an account can
// take, which are locked and what a category switch does. The catalog comes
// from GET /api/v1/users (key, category, requires, https_only, guest_allowed,
// ha_allowed) and is passed in on every call. No DOM and no texts here,
// users.page.js does the drawing. Spec: .docs/superpowers/specs/2026-09-03-benutzerverwaltung-design.md, section 8.
(() => {
  // Kinds of account the picker knows: a local account, the guest access, the
  // Home Assistant preset rows (ha_set) and a Home Assistant account (ha).
  const KIND_FILTERS = Object.freeze({
    local: () => true,
    guest: entry => entry.guest_allowed,
    ha_set: entry => entry.ha_allowed,
    ha: entry => entry.ha_allowed,
  });

  const sortedUnique = keys => [...new Set(keys)].sort();

  // available(catalog, kind): the entries a kind may hold.
  function available(catalog, kind) {
    const keep = KIND_FILTERS[kind] || KIND_FILTERS.local;
    return catalog.filter(keep);
  }

  // normalize(catalog, roles): like Go Normalize. Drops unknown keys and
  // duplicates, and drops every permission whose requires is not held.
  function normalize(catalog, roles) {
    const known = new Map(catalog.map(entry => [entry.key, entry]));
    const present = new Set(roles.filter(key => known.has(key)));
    return sortedUnique([...present].filter(key => {
      const requires = known.get(key).requires;
      return !requires || present.has(requires);
    }));
  }

  // locks(catalog, account, draftRoles, context): {[key]: reason}. reason is
  // 'requires' (the permission it needs is not held), 'last_admin', 'self' or
  // 'preset' (a Home Assistant account without its own selection follows the
  // preset, so every entry it may hold is locked).
  // context = {self: boolean, otherActiveAdmins: number}, counting the other
  // active local accounts that hold user_management.
  function locks(catalog, account, draftRoles, context) {
    const result = {};
    const held = new Set(draftRoles);
    catalog.forEach(entry => {
      if (entry.requires && !held.has(entry.requires)) result[entry.key] = 'requires';
    });
    if (account.kind === 'local' && held.has('user_management') && context.otherActiveAdmins === 0) {
      result.user_management = 'last_admin';
      result.__active = 'last_admin';
    }
    if (context.self) result.__active = 'self';
    if (account.kind === 'ha' && !account.custom) {
      catalog.forEach(entry => {
        if (entry.ha_allowed) result[entry.key] = 'preset';
      });
    }
    return result;
  }

  // groupState(catalog, kind, category, roles): how many of the category's
  // entries that the kind may hold are on.
  function groupState(catalog, kind, category, roles) {
    const held = new Set(roles);
    const entries = available(catalog, kind).filter(entry => entry.category === category);
    const on = entries.filter(entry => held.has(entry.key)).length;
    const of = entries.length;
    let state = 'mixed';
    if (on === 0) state = 'off';
    else if (on === of) state = 'on';
    return {on, of, state};
  }

  // toggleGroup(catalog, account, draftRoles, category, context): the new role
  // list after the category switch. Off or mixed switches every allowed, not
  // locked entry on. On switches them all off. If switching on changes
  // nothing, it switches off instead.
  function toggleGroup(catalog, account, draftRoles, category, context) {
    const kind = account.kind;
    const entries = available(catalog, kind).filter(entry => entry.category === category);
    const base = normalize(catalog, draftRoles);

    const apply = (on, roles) => {
      let next = [...roles];
      entries.forEach(entry => {
        const locked = locks(catalog, account, next, context)[entry.key];
        if (locked) return;
        if (on) next = [...next, entry.key];
        else next = next.filter(key => key !== entry.key);
        next = normalize(catalog, next);
      });
      return normalize(catalog, next);
    };

    const wantOn = groupState(catalog, kind, category, base).state !== 'on';
    let next = apply(wantOn, base);
    if (wantOn && next.join('\n') === base.join('\n')) next = apply(false, base);
    return next;
  }

  // httpWarning(catalog, roles, hasHTTPPassword): null when the account has no
  // http_login or holds no HTTPS-only permission. Otherwise the held
  // HTTPS-only keys in catalog order, which HTTP sessions do not get, and
  // whether a separate HTTP password is set.
  function httpWarning(catalog, roles, hasHTTPPassword) {
    const held = new Set(roles);
    if (!held.has('http_login')) return null;
    const withheld = catalog
      .filter(entry => entry.https_only && held.has(entry.key))
      .map(entry => entry.key);
    if (withheld.length === 0) return null;
    return {withheld, safe: Boolean(hasHTTPPassword)};
  }

  // summary(catalog, kind, roles): one row per category with enabled entries.
  // label is 'all' when every available entry is on, otherwise 'n_of_m'.
  function summary(catalog, kind, roles) {
    const categories = [];
    catalog.forEach(entry => {
      if (!categories.includes(entry.category)) categories.push(entry.category);
    });
    return categories.reduce((rows, category) => {
      const {on, of} = groupState(catalog, kind, category, roles);
      if (on === 0) return rows;
      rows.push({category, label: on === of ? 'all' : 'n_of_m', n: on, m: of});
      return rows;
    }, []);
  }

  // elevatedKeys(catalog, roles): the held permissions with the elevated flag
  // (system restart or update, user management, MQTT, Tailscale), catalog order.
  function elevatedKeys(catalog, roles) {
    const held = new Set(roles);
    return catalog
      .filter(entry => entry.elevated && held.has(entry.key))
      .map(entry => entry.key);
  }

  // newlyElevated(catalog, oldRoles, newRoles): the elevated keys a save adds.
  // Keys that were already held are not listed again.
  function newlyElevated(catalog, oldRoles, newRoles) {
    const before = new Set(oldRoles);
    return elevatedKeys(catalog, newRoles).filter(key => !before.has(key));
  }

  // groupSessions(sessions): {username: [session, ...]} in the order the server
  // sent them.
  function groupSessions(sessions) {
    return sessions.reduce((groups, session) => {
      (groups[session.username] = groups[session.username] || []).push(session);
      return groups;
    }, {});
  }

  window.UsersModel = {
    available, normalize, locks, groupState, toggleGroup, httpWarning, summary,
    elevatedKeys, newlyElevated, groupSessions,
  };
})();
