<?php
try {
    // проверка, что пришел POST запрос
    if ($_SERVER["REQUEST_METHOD"] == "POST") {
        // файл с подключением к базе данных
        require_once 'database_connection.php';

        // проверка соединения
        if ($conn->connect_error) {
            die("Соединение не удалось: " . $conn->connect_error);
        }
        // получение данных из тела запроса
        $data = json_decode(file_get_contents('php://input'), true);

        // подготовка SQL запроса для вставки в таблицу
        $sql = "INSERT INTO stat (total_size, date_time, root_path, load_time_seconds) VALUES (?, ?, ?, ?)";
        $stmt = $conn->prepare($sql);
        $stmt->bind_param("isss", $data['totalSize'], $data['date'], $data['path'], $data['loadTime']);
        
        // выполнение SQL запроса
        $stmt->execute();
        $stmt->close();
        
        // закрытие соединения с базой данных
        $conn->close();
    } else {
        // если запрос не POST, отправка сообщения об ошибке
        http_response_code(405);
        echo "Некорректный запрос (неверный тип, ожидался POST)";
    }
} catch (Exception $e) {
    http_response_code(500);
    header("Content-Type:  application/json");
    echo json_encode(array('message' => $e->getMessage()));
}
?>
