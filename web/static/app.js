// AutoMarket — mejoras progresivas de la interfaz. Todas las páginas funcionan
// sin JavaScript; este archivo añade búsqueda, orden, detalle en modal, vista
// previa de fotos y confirmación en dos pasos para las acciones destructivas.
(function () {
	"use strict";

	// ---------- Catálogo: búsqueda y orden ----------
	var grid = document.querySelector("[data-catalog]");
	if (grid) {
		var cards = Array.prototype.slice.call(grid.querySelectorAll("[data-card]"));
		var search = document.querySelector("[data-search]");
		var count = document.querySelector("[data-count]");
		var empty = document.querySelector("[data-empty]");
		var sortButtons = document.querySelectorAll("[data-sort]");
		var sortBy = "recent";

		var label = function (n) {
			if (n === 0) return "Ningún vehículo disponible";
			return n === 1 ? "1 vehículo disponible" : n + " vehículos disponibles";
		};

		var apply = function () {
			var q = search ? search.value.trim().toLowerCase() : "";
			var visible = 0;
			cards.forEach(function (card) {
				var match = !q || card.dataset.search.indexOf(q) !== -1;
				card.hidden = !match;
				if (match) visible++;
			});
			var sorted = cards.slice().sort(function (a, b) {
				if (sortBy === "low") return a.dataset.price - b.dataset.price;
				if (sortBy === "high") return b.dataset.price - a.dataset.price;
				return b.dataset.created - a.dataset.created;
			});
			sorted.forEach(function (card) { grid.appendChild(card); });
			if (count) count.textContent = label(visible);
			if (empty) empty.hidden = visible !== 0;
			grid.hidden = visible === 0;
		};

		if (search) search.addEventListener("input", apply);
		sortButtons.forEach(function (btn) {
			btn.addEventListener("click", function () {
				sortBy = btn.dataset.sort;
				sortButtons.forEach(function (b) {
					var on = b === btn;
					b.classList.toggle("is-active", on);
					b.setAttribute("aria-pressed", on ? "true" : "false");
				});
				apply();
			});
		});
		var clear = document.querySelector("[data-clear]");
		if (clear) clear.addEventListener("click", function () {
			if (search) { search.value = ""; search.focus(); }
			apply();
		});
		apply();
	}

	// ---------- Detalle del vehículo (modal) ----------
	document.querySelectorAll("[data-open]").forEach(function (btn) {
		btn.addEventListener("click", function () {
			var dialog = document.getElementById(btn.dataset.open);
			if (dialog && dialog.showModal) dialog.showModal();
		});
	});
	document.querySelectorAll("dialog.detail").forEach(function (dialog) {
		// Cerrar al hacer clic fuera de la ficha.
		dialog.addEventListener("click", function (e) {
			if (e.target === dialog) dialog.close();
		});
		var main = dialog.querySelector("[data-main]");
		var thumbs = dialog.querySelectorAll("[data-thumb]");
		thumbs.forEach(function (t) {
			t.addEventListener("click", function () {
				if (main) main.src = t.dataset.thumb;
				thumbs.forEach(function (o) { o.classList.toggle("is-active", o === t); });
			});
		});
	});

	// ---------- Selector de fotos con vista previa ----------
	var input = document.querySelector("[data-photo-input]");
	var picker = document.querySelector("[data-photo-picker]");
	if (input && picker && window.DataTransfer) {
		var MAX = 22, MAX_BYTES = 5 * 1024 * 1024;
		var OK_TYPES = ["image/jpeg", "image/png", "image/webp", "image/gif"];
		var files = [];
		var addBtn = picker.querySelector("[data-photo-add]");
		var counter = document.querySelector("[data-photo-counter]");
		var notice = document.querySelector("[data-photo-error]");

		var sync = function () {
			var dt = new DataTransfer();
			files.forEach(function (f) { dt.items.add(f); });
			input.files = dt.files;

			picker.querySelectorAll(".thumb").forEach(function (n) {
				URL.revokeObjectURL(n.querySelector("img").src);
				n.remove();
			});
			files.forEach(function (f, i) {
				var box = document.createElement("div");
				box.className = "thumb";
				var img = document.createElement("img");
				img.src = URL.createObjectURL(f);
				img.alt = "Foto " + (i + 1);
				var rm = document.createElement("button");
				rm.type = "button";
				rm.title = "Quitar";
				rm.setAttribute("aria-label", "Quitar foto " + (i + 1));
				rm.textContent = "×";
				rm.addEventListener("click", function () { files.splice(i, 1); sync(); });
				box.appendChild(img);
				box.appendChild(rm);
				picker.insertBefore(box, addBtn);
			});
			addBtn.hidden = files.length >= MAX;
			if (counter) counter.textContent = files.length + " / " + MAX;
		};

		var showError = function (msg) {
			if (!notice) return;
			notice.textContent = msg;
			notice.hidden = !msg;
		};

		addBtn.addEventListener("click", function () { input.click(); });
		input.addEventListener("change", function () {
			// input.files ya fue reemplazado por la nueva selección.
			var picked = Array.prototype.slice.call(input.files);
			var valid = picked.filter(function (f) { return OK_TYPES.indexOf(f.type) !== -1 && f.size <= MAX_BYTES; });
			var room = MAX - files.length;
			var msg = "";
			if (valid.length < picked.length) msg = "Algunas imágenes no se agregaron: formato no admitido o más de 5 MB.";
			if (valid.length > room) msg = "Solo se pueden subir " + MAX + " fotos. Agregamos las primeras " + room + ".";
			files = files.concat(valid.slice(0, room));
			showError(msg);
			sync();
		});
		sync();
	}

	// ---------- Confirmación en dos pasos ----------
	document.querySelectorAll("[data-confirm]").forEach(function (btn) {
		var original = btn.textContent;
		var timer = null;
		btn.addEventListener("click", function (e) {
			if (btn.classList.contains("is-confirming")) return; // segundo clic: enviar
			e.preventDefault();
			btn.classList.add("is-confirming");
			btn.textContent = btn.dataset.confirm;
			clearTimeout(timer);
			timer = setTimeout(function () {
				btn.classList.remove("is-confirming");
				btn.textContent = original;
			}, 3000);
		});
	});
})();
