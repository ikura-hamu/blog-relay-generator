USE blog_relay;

CREATE TABLE IF NOT EXISTS words (
  id INT AUTO_INCREMENT PRIMARY KEY,
  word VARCHAR(256) NOT NULL,
  word_type VARCHAR(255) NOT NULL,
  word_type2 VARCHAR(255)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS sentences (
  id INT AUTO_INCREMENT PRIMARY KEY,
  word_types TEXT NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE INDEX word_type_index ON words (word_type, word_type2);

CREATE TABLE IF NOT EXISTS posts_count (
  id INT AUTO_INCREMENT PRIMARY KEY,
  count INT NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS likes (
  id INT AUTO_INCREMENT PRIMARY KEY,
  theme TEXT NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

ALTER TABLE `likes`
ADD UNIQUE `theme_unique` (`theme`(1));