-- Where a click on one source's entries goes. open_target is follow (the kind decides, the default), reader (the
-- reader pane) or browser (the system browser). See docs/product.md "时间线", "点开时".

ALTER TABLE sources ADD COLUMN open_target TEXT NOT NULL DEFAULT 'follow';
