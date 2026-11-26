/* Read-only snapshots in IndexedDB. All failures degrade to normal requests. */
(() => {
  for(const name of ["sessionStorage","localStorage"]){try{const storage=window[name];for(const key of Object.keys(storage))if(key.startsWith("govmc-cache:v1:"))storage.removeItem(key);}catch{}}
  const ttl = 120000,
    maxEntries = 64,
    memory = new Map();
  const newTab = () =>
    window.crypto?.randomUUID?.() ||
    Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
  let tab;
  try {
    tab = sessionStorage.getItem("govmc-tab-id");
    if (!tab) {
      tab = newTab();
      sessionStorage.setItem("govmc-tab-id", tab);
    }
  } catch {
    tab = newTab();
  }
  const prefix = (instance, profile) =>
    encodeURIComponent(instance) + ":" + encodeURIComponent(profile) + ":";
  const key = (instance, profile, name) =>
    prefix(instance, profile) + encodeURIComponent(name);
  const ready = new Promise((resolve) => {
    try {
      const request = indexedDB.open("govmc-read-cache", 1);
      request.onupgradeneeded = () => {
        const store = request.result.createObjectStore("snapshots", {
          keyPath: "key",
        });
        store.createIndex("expires", "expires");
      };
      request.onsuccess = () => {
        request.result.onversionchange = () => request.result.close();
        resolve(request.result);
      };
      request.onerror = () => resolve(null);
      request.onblocked = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
  async function transaction(mode, run) {
    const db = await ready;
    if (!db) return null;
    return new Promise((resolve) => {
      try {
        const tx = db.transaction("snapshots", mode),
          store = tx.objectStore("snapshots");
        let result = null;
        run(store, (value) => {
          result = value;
        });
        tx.oncomplete = () => resolve(result);
        tx.onerror = tx.onabort = () => resolve(null);
      } catch {
        resolve(null);
      }
    });
  }
  async function cleanup() {
    await transaction("readwrite", (store) => {
      const expired = store
        .index("expires")
        .openKeyCursor(IDBKeyRange.upperBound(Date.now()));
      expired.onsuccess = () => {
        const cursor = expired.result;
        if (cursor) {
          store.delete(cursor.primaryKey);
          cursor.continue();
        } else {
          const count = store.count();
          count.onsuccess = () => {
            let remove = count.result - maxEntries;
            if (remove <= 0) return;
            const oldest = store.index("expires").openKeyCursor();
            oldest.onsuccess = () => {
              const cursor = oldest.result;
              if (cursor && remove-- > 0) {
                store.delete(cursor.primaryKey);
                cursor.continue();
              }
            };
          };
        }
      };
    });
  }
  async function read(instance, profile, name) {
    if (!instance || !profile) return null;
    const id = key(instance, profile, name),
      record =
        (await transaction("readonly", (store, done) => {
          const req = store.get(id);
          req.onsuccess = () => done(req.result);
        })) || memory.get(id);
    if (!record || record.expires < Date.now() || record.at > Date.now() + 1000)
      return null;
    return { at: record.at, value: record.value };
  }
  async function write(instance, profile, name, value) {
    if (!instance || !profile) return false;
    const record = {
      key: key(instance, profile, name),
      at: Date.now(),
      expires: Date.now() + ttl,
      value,
    };
    const result = await transaction("readwrite", (store, done) => {
      store.put(record);
      done(true);
    });
    if (!result) {
      memory.set(record.key, record);
      while (memory.size > 8) memory.delete(memory.keys().next().value);
    } else memory.delete(record.key);
    await cleanup();
    return !!result;
  }
  async function invalidate(instance, profile) {
    const start = prefix(instance, profile);
    for (const id of memory.keys()) if (id.startsWith(start)) memory.delete(id);
    await transaction("readwrite", (store) =>
      store.delete(IDBKeyRange.bound(start, start + "\uffff")),
    );
  }
  cleanup();
  window.govmcCache = { read, write, invalidate, tab };
})();
