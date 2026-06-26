INSERT INTO categories (name, category_type, icon, color, is_system, display_order) VALUES
    -- Ingresos
    ('Salario',            'income',  'briefcase',      '#4CAF50', TRUE, 1),
    ('Freelance',          'income',  'laptop',         '#66BB6A', TRUE, 2),
    ('Inversiones',        'income',  'trending-up',    '#43A047', TRUE, 3),
    ('Regalos',            'income',  'gift',           '#81C784', TRUE, 4),
    ('Reembolsos',         'income',  'arrow-back',     '#A5D6A7', TRUE, 5),
    ('Otros ingresos',     'income',  'plus-circle',    '#C8E6C9', TRUE, 6),

    -- Gastos
    ('Alimentación',       'expense', 'restaurant',     '#FF7043', TRUE, 10),
    ('Transporte',         'expense', 'directions-car', '#42A5F5', TRUE, 11),
    ('Vivienda',           'expense', 'home',           '#8D6E63', TRUE, 12),
    ('Servicios públicos', 'expense', 'flash',          '#FFA726', TRUE, 13),
    ('Salud',              'expense', 'medical-bag',    '#EF5350', TRUE, 14),
    ('Educación',          'expense', 'school',         '#5C6BC0', TRUE, 15),
    ('Entretenimiento',    'expense', 'movie',          '#AB47BC', TRUE, 16),
    ('Suscripciones',      'expense', 'card',           '#7E57C2', TRUE, 17),
    ('Ropa',               'expense', 'shirt',          '#EC407A', TRUE, 18),
    ('Mascotas',           'expense', 'paw',            '#26A69A', TRUE, 19),
    ('Café y snacks',      'expense', 'coffee',         '#A1887F', TRUE, 20),
    ('Tecnología',         'expense', 'laptop',         '#78909C', TRUE, 21),
    ('Impuestos',          'expense', 'document-text',  '#546E7A', TRUE, 22),
    ('Otros gastos',       'expense', 'help-circle',    '#BDBDBD', TRUE, 23);
