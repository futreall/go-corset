(defcolumns
  (X :u16)
  (Y :u16))
(permute (A B) (+X +Y))
(vanish diag_ab (- (shift A 1) B))
