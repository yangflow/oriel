-- Pace per source. pace_sec is how long an unread entry of the source stays in All and Unread, counted from its
-- published time (else the time it was fetched): 0 turns the pace off (the default), a positive number is seconds.
-- Past it the entry leaves those two views and their counts, and stays in search, Starred and the source's own view.
-- Nothing is marked read or deleted. See docs/product.md "节奏".

ALTER TABLE sources ADD COLUMN pace_sec INTEGER NOT NULL DEFAULT 0 CHECK (pace_sec >= 0);
